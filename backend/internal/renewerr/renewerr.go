// Package renewerr 把续期过程中的各类错误分类,并据此决定重试节奏。
//
// 分类的意义:域名归属校验失败、AK 失效这类错误重试一万次也不会成功,
// 只会每轮浪费一次 Let's Encrypt 配额并制造一条告警;而限流、网络抖动值得退避后再来。
package renewerr

import (
	"context"
	"errors"
	"fmt"
	rand "math/rand/v2"
	"net"
	"time"

	dara "github.com/alibabacloud-go/tea/dara"
	tea "github.com/alibabacloud-go/tea/tea"
	alioss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/go-acme/lego/v4/acme"

	entdomain "github.com/solarhell/certship/pkg/ent/domain"
)

const (
	// baseDelay 首次可重试错误的退避起点,之后按 2 的幂增长
	baseDelay = 30 * time.Minute
	// maxDelay 单次退避上限
	maxDelay = 24 * time.Hour
	// rateLimitedDelay 被限速后的固定等待。要大于一个扫描周期,
	// 否则 Let's Encrypt 的 7 天滚动窗口会被每日重试持续撑满。
	rateLimitedDelay = 48 * time.Hour
	// maxAttempts 连续失败多少次后不再自动重试,转为需要人工介入
	maxAttempts = 8
)

// permanentOSSCodes OSS 侧重试无意义的错误码
var permanentOSSCodes = map[string]struct{}{
	"NeedVerifyDomainOwnership": {}, // 域名归属校验不过:解析已摘掉或未配 CNAME/TXT
	"AccessDenied":              {},
	"InvalidAccessKeyId":        {},
	"SignatureDoesNotMatch":     {},
	"SecurityTokenExpired":      {},
	"NoSuchBucket":              {},
	"NoSuchCname":               {},
	"CnameNotExist":             {},
	"InvalidArgument":           {},
	"MalformedXML":              {},
}

// permanentAliCodes 阿里云 OpenAPI(CDN 等)侧重试无意义的错误码
var permanentAliCodes = map[string]struct{}{
	"InvalidDomain.NotFound":      {},
	"InvalidDomain.Offline":       {},
	"InvalidDomain.NotOnline":     {},
	"DomainNotFound":              {},
	"Forbidden":                   {},
	"Forbidden.NotAllowed":        {},
	"Unauthorized":                {},
	"InvalidAccessKeyId.NotFound": {},
	"SignatureDoesNotMatch":       {},
	"InvalidCertificate":          {},
}

// rateLimitedAliCodes 阿里云侧的限流错误码
var rateLimitedAliCodes = map[string]struct{}{
	"Throttling":      {},
	"Throttling.User": {},
	"Throttling.Api":  {},
	"ServiceBusy":     {},
}

// permanentACMETypes ACME 侧需要人工介入的问题类型
var permanentACMETypes = map[string]struct{}{
	"urn:ietf:params:acme:error:malformed":             {},
	"urn:ietf:params:acme:error:unauthorized":          {},
	"urn:ietf:params:acme:error:accountDoesNotExist":   {},
	"urn:ietf:params:acme:error:caa":                   {},
	"urn:ietf:params:acme:error:rejectedIdentifier":    {},
	"urn:ietf:params:acme:error:invalidContact":        {},
	"urn:ietf:params:acme:error:unsupportedIdentifier": {},
}

// permanentError 显式标记"重试也没用"的错误。
// 用于 SDK 之外我们自己判定出的死局,比如域名早已从 bucket 解绑。
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }

func (e *permanentError) Unwrap() error { return e.err }

// Permanent 把一个错误标记为无需重试
func Permanent(format string, args ...any) error {
	return &permanentError{err: fmt.Errorf(format, args...)}
}

// Classify 判断错误属于哪一类。无法识别的错误按可重试处理——
// 宁可多试几次,也不要把一个偶发故障永久标记成需要人工介入。
func Classify(err error) entdomain.ErrorKind {
	if err == nil {
		return entdomain.ErrorKindNone
	}

	// context 取消是进程自身的调度行为,不该计入域名的失败次数
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return entdomain.ErrorKindTransient
	}

	if _, ok := errors.AsType[*permanentError](err); ok {
		return entdomain.ErrorKindPermanent
	}

	if ossErr, ok := errors.AsType[*alioss.ServiceError](err); ok {
		if _, ok := permanentOSSCodes[ossErr.Code]; ok {
			return entdomain.ErrorKindPermanent
		}
		return byStatusCode(ossErr.StatusCode)
	}

	if code, status, ok := aliyunError(err); ok {
		if _, bad := permanentAliCodes[code]; bad {
			return entdomain.ErrorKindPermanent
		}
		if _, limited := rateLimitedAliCodes[code]; limited {
			return entdomain.ErrorKindRateLimited
		}
		return byStatusCode(status)
	}

	if problem, ok := errors.AsType[*acme.ProblemDetails](err); ok {
		if problem.Type == "urn:ietf:params:acme:error:rateLimited" {
			return entdomain.ErrorKindRateLimited
		}
		if _, ok := permanentACMETypes[problem.Type]; ok {
			return entdomain.ErrorKindPermanent
		}
		return entdomain.ErrorKindTransient
	}

	if _, ok := errors.AsType[net.Error](err); ok {
		return entdomain.ErrorKindTransient
	}

	return entdomain.ErrorKindTransient
}

// aliyunError 从 tea / darabonba 两种 SDK 错误里取出错误码与 HTTP 状态码
func aliyunError(err error) (code string, status int, ok bool) {
	if teaErr, ok := errors.AsType[*tea.SDKError](err); ok {
		return derefString(teaErr.Code), derefInt(teaErr.StatusCode), true
	}
	if daraErr, ok := errors.AsType[*dara.SDKError](err); ok {
		return derefString(daraErr.Code), derefInt(daraErr.StatusCode), true
	}
	return "", 0, false
}

// byStatusCode 没有可识别错误码时,退回用 HTTP 状态码判断
func byStatusCode(status int) entdomain.ErrorKind {
	switch {
	case status == 429:
		return entdomain.ErrorKindRateLimited
	case status >= 500:
		return entdomain.ErrorKindTransient
	case status == 403 || status == 404 || status == 400:
		return entdomain.ErrorKindPermanent
	default:
		return entdomain.ErrorKindTransient
	}
}

// Backoff 根据错误分类与已连续失败次数,给出下次允许重试的时间。
// 返回 nil 表示不再自动重试,需要人工处理。
//
// attempts 是包含本次失败在内的连续失败次数。
func Backoff(kind entdomain.ErrorKind, attempts int, now time.Time) *time.Time {
	switch kind {
	case entdomain.ErrorKindPermanent:
		return nil
	case entdomain.ErrorKindRateLimited:
		t := now.Add(withJitter(rateLimitedDelay))
		return &t
	case entdomain.ErrorKindTransient:
		if attempts >= maxAttempts {
			return nil
		}
		delay := baseDelay << min(attempts-1, 16)
		if delay > maxDelay || delay <= 0 {
			delay = maxDelay
		}
		t := now.Add(withJitter(delay))
		return &t
	default:
		return nil
	}
}

// Exhausted 报告可重试错误是否已经试到上限,该转人工了
func Exhausted(kind entdomain.ErrorKind, attempts int) bool {
	return kind == entdomain.ErrorKindPermanent ||
		(kind == entdomain.ErrorKindTransient && attempts >= maxAttempts)
}

// withJitter 给退避加 ±10% 抖动,避免大批域名在同一时刻一起重试
func withJitter(d time.Duration) time.Duration {
	jitter := float64(d) * 0.1
	return d + time.Duration((rand.Float64()*2-1)*jitter)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}
