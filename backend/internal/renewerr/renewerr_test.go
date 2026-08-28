package renewerr

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	tea "github.com/alibabacloud-go/tea/tea"
	alioss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/go-acme/lego/v4/acme"

	entdomain "github.com/solarhell/certship/pkg/ent/domain"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want entdomain.ErrorKind
	}{
		{"nil 不是错误", nil, entdomain.ErrorKindNone},
		{
			// 线上真实场景:域名解析已摘掉,OSS 拒绝绑定证书。再签一万张也没用。
			"域名归属校验失败是永久错误",
			fmt.Errorf("put cname: %w", &alioss.ServiceError{Code: "NeedVerifyDomainOwnership", StatusCode: 403}),
			entdomain.ErrorKindPermanent,
		},
		{"AK 失效是永久错误", &alioss.ServiceError{Code: "InvalidAccessKeyId", StatusCode: 403}, entdomain.ErrorKindPermanent},
		{"OSS 服务端错误可重试", &alioss.ServiceError{Code: "InternalError", StatusCode: 500}, entdomain.ErrorKindTransient},
		{"OSS 429 是限速", &alioss.ServiceError{Code: "TooManyRequests", StatusCode: 429}, entdomain.ErrorKindRateLimited},
		{"CDN 域名不存在是永久错误", teaError("InvalidDomain.NotFound", 404), entdomain.ErrorKindPermanent},
		{"CDN 限流", teaError("Throttling.User", 400), entdomain.ErrorKindRateLimited},
		{"CDN 服务端错误可重试", teaError("InternalError", 500), entdomain.ErrorKindTransient},
		{
			"ACME 限速",
			fmt.Errorf("obtain: %w", &acme.ProblemDetails{Type: "urn:ietf:params:acme:error:rateLimited"}),
			entdomain.ErrorKindRateLimited,
		},
		{
			"ACME 授权失败需人工",
			&acme.ProblemDetails{Type: "urn:ietf:params:acme:error:unauthorized"},
			entdomain.ErrorKindPermanent,
		},
		{"显式标记的永久错误", Permanent("域名 %s 已解绑", "a.example.com"), entdomain.ErrorKindPermanent},
		{"包了一层的永久错误仍可识别", fmt.Errorf("预检失败: %w", Permanent("已解绑")), entdomain.ErrorKindPermanent},
		{"context 取消不算域名的错", context.Canceled, entdomain.ErrorKindTransient},
		{"未知错误保守按可重试处理", errors.New("boom"), entdomain.ErrorKindTransient},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != tc.want {
				t.Errorf("Classify() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	if got := Backoff(entdomain.ErrorKindPermanent, 1, now); got != nil {
		t.Errorf("永久错误不该再安排重试,got %v", got)
	}

	// 可重试错误的间隔应当逐次拉长
	var prev time.Duration
	for attempts := 1; attempts <= 5; attempts++ {
		next := Backoff(entdomain.ErrorKindTransient, attempts, now)
		if next == nil {
			t.Fatalf("第 %d 次失败不该停止重试", attempts)
		}
		delay := next.Sub(now)
		if delay <= prev {
			t.Errorf("第 %d 次退避 %v 没有比上一次 %v 更长", attempts, delay, prev)
		}
		if delay > maxDelay*2 {
			t.Errorf("第 %d 次退避 %v 超出上限", attempts, delay)
		}
		prev = delay
	}

	if got := Backoff(entdomain.ErrorKindTransient, maxAttempts, now); got != nil {
		t.Errorf("重试到上限后应转人工,got %v", got)
	}

	// 限速的等待必须跨过一个扫描周期,否则每日重试会持续撑满 LE 的滚动窗口
	next := Backoff(entdomain.ErrorKindRateLimited, 1, now)
	if next == nil {
		t.Fatal("限速应当安排稍后重试")
	}
	if next.Sub(now) < 24*time.Hour {
		t.Errorf("限速退避 %v 太短", next.Sub(now))
	}
}

func TestExhausted(t *testing.T) {
	if !Exhausted(entdomain.ErrorKindPermanent, 1) {
		t.Error("永久错误应当直接转人工")
	}
	if Exhausted(entdomain.ErrorKindTransient, 1) {
		t.Error("首次可重试错误不该转人工")
	}
	if !Exhausted(entdomain.ErrorKindTransient, maxAttempts) {
		t.Error("重试到上限应当转人工")
	}
}

// 退避必须给出确定性的上界:大量域名同时失败时,抖动不能把重试时间推到离谱的位置。
func TestBackoffJitterStaysBounded(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	for range 200 {
		next := Backoff(entdomain.ErrorKindTransient, 3, now)
		if next == nil {
			t.Fatal("第 3 次失败不该停止重试")
		}
		delay := next.Sub(now)
		want := baseDelay << 2 // attempts-1 = 2
		if delay < time.Duration(float64(want)*0.9) || delay > time.Duration(float64(want)*1.1) {
			t.Fatalf("退避 %v 超出 %v 的 ±10%% 抖动范围", delay, want)
		}
	}
}

func teaError(code string, status int) error {
	return &tea.SDKError{Code: &code, StatusCode: &status}
}
