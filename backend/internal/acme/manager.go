package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/alidns"
	"github.com/go-acme/lego/v4/registration"
	"go.uber.org/zap"
)

// accountEmail 是向 CA 注册时使用的联系邮箱,固定不可配置。
//
// directoryURL 同样固定:它是全局设置,改成 staging 会让所有域名签出不被信任的证书
// 并自动部署到线上;而且 ACME 账号是绑 CA 的,切换后存量注册信息立刻失效。
// 要演练就另起一个实例连独立的库,不要在生产实例上切开关。
const (
	accountEmail = "acme@certship.dev"
	directoryURL = lego.LEDirectoryProduction
)

// user 实现 registration.User 接口
type user struct {
	Email        string
	Registration *registration.Resource
	key          crypto.PrivateKey
}

func (u *user) GetEmail() string { return u.Email }

func (u *user) GetRegistration() *registration.Resource { return u.Registration }

func (u *user) GetPrivateKey() crypto.PrivateKey { return u.key }

// CertResult 保存申请结果，AccountKeyPEM 和 RegistrationJSON 应持久化回数据库
type CertResult struct {
	CertPEM          string
	KeyPEM           string
	AccountKeyPEM    string // 账号私钥 PEM（每次均返回，供调用方持久化）
	RegistrationJSON string // 注册信息 JSON（每次均返回）
}

type Manager struct {
	logger *zap.Logger
}

func NewManager(logger *zap.Logger) *Manager {
	return &Manager{logger: logger}
}

// Account 是一个已注册的 ACME 账号,可安全地被多个并发签发共用。
//
// 拆出来是为了让"注册"只发生一次:并发续期时若每个任务各自注册,
// 会重复向 CA 建账号,也会各自把不同的账号密钥写回数据库。
type Account struct {
	user *user
	// KeyPEM / RegistrationJSON 需要持久化回数据库
	KeyPEM           string
	RegistrationJSON string
	// Registered 表示本次是新注册的,调用方据此决定是否写库
	Registered bool
}

// ObtainOptions 描述一次证书申请所需的全部输入
type ObtainOptions struct {
	Domains         []string
	AccessKeyID     string
	AccessKeySecret string
	// Resolvers 是校验 TXT 记录是否传播时使用的递归 DNS(host:port)。
	// 与 zone 探测用同一组,避免"这边说记录好了、那边说没看到"的割裂。
	Resolvers []string
}

// EnsureAccount 复用数据库里的 ACME 账号,没有则注册一个。
//
// accountKeyPEM / registrationJSON 从数据库传入(首次为空)。
// 返回的账号可以并发用于多个 ObtainCert。
func (m *Manager) EnsureAccount(accountKeyPEM, registrationJSON string) (*Account, error) {
	u, err := m.buildUser(accountKeyPEM, registrationJSON)
	if err != nil {
		return nil, fmt.Errorf("build acme user: %w", err)
	}

	keyPEMBytes := pem.EncodeToMemory(certcrypto.PEMBlock(u.key))
	account := &Account{user: u, KeyPEM: string(keyPEMBytes)}

	if u.Registration != nil {
		account.RegistrationJSON = registrationJSON
		return account, nil
	}

	client, err := m.newClient(u)
	if err != nil {
		return nil, err
	}
	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, fmt.Errorf("register acme account: %w", err)
	}
	u.Registration = reg
	m.logger.Info("ACME 账号注册成功", zap.String("email", accountEmail))

	regJSON, err := json.Marshal(reg)
	if err != nil {
		return nil, fmt.Errorf("marshal acme registration: %w", err)
	}
	account.RegistrationJSON = string(regJSON)
	account.Registered = true
	return account, nil
}

// ObtainCert 使用阿里云 DNS-01 挑战为 opts.Domains 申请证书（支持 SAN 多域名）。
// account 由 EnsureAccount 提供,可被并发调用共用。
func (m *Manager) ObtainCert(account *Account, opts ObtainOptions) (*CertResult, error) {
	client, err := m.newClient(account.user)
	if err != nil {
		return nil, err
	}

	dnsCfg := alidns.NewDefaultConfig()
	dnsCfg.APIKey = opts.AccessKeyID
	dnsCfg.SecretKey = opts.AccessKeySecret
	provider, err := alidns.NewDNSProviderConfig(dnsCfg)
	if err != nil {
		return nil, fmt.Errorf("create alidns provider: %w", err)
	}
	var challengeOpts []dns01.ChallengeOption
	if len(opts.Resolvers) > 0 {
		challengeOpts = append(challengeOpts, dns01.AddRecursiveNameservers(opts.Resolvers))
	}
	if err := client.Challenge.SetDNS01Provider(provider, challengeOpts...); err != nil {
		return nil, fmt.Errorf("set dns01 provider: %w", err)
	}

	m.logger.Info("开始申请证书", zap.Strings("domains", opts.Domains))
	certs, err := client.Certificate.Obtain(certificate.ObtainRequest{
		Domains: opts.Domains,
		Bundle:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("obtain cert for %v: %w", opts.Domains, err)
	}
	m.logger.Info("证书申请成功", zap.Strings("domains", opts.Domains))

	return &CertResult{
		CertPEM:          string(certs.Certificate),
		KeyPEM:           string(certs.PrivateKey),
		AccountKeyPEM:    account.KeyPEM,
		RegistrationJSON: account.RegistrationJSON,
	}, nil
}

// newClient 建立指向 Let's Encrypt 生产环境的 lego client
func (m *Manager) newClient(u *user) (*lego.Client, error) {
	legoCfg := lego.NewConfig(u)
	legoCfg.Certificate.KeyType = certcrypto.RSA2048
	legoCfg.CADirURL = directoryURL
	client, err := lego.NewClient(legoCfg)
	if err != nil {
		return nil, fmt.Errorf("create lego client: %w", err)
	}
	return client, nil
}

func (m *Manager) buildUser(accountKeyPEM, registrationJSON string) (*user, error) {
	var privateKey crypto.PrivateKey

	if accountKeyPEM != "" {
		key, err := certcrypto.ParsePEMPrivateKey([]byte(accountKeyPEM))
		if err != nil {
			return nil, fmt.Errorf("parse account key: %w", err)
		}
		privateKey = key
	} else {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate account key: %w", err)
		}
		privateKey = key
		m.logger.Info("已生成新的 ACME 账号密钥")
	}

	u := &user{Email: accountEmail, key: privateKey}

	if registrationJSON != "" {
		var reg registration.Resource
		if err := json.Unmarshal([]byte(registrationJSON), &reg); err == nil {
			u.Registration = &reg
		}
	}

	return u, nil
}

// ParseCertExpiry 解析 PEM 格式证书的到期时间
func ParseCertExpiry(certPEM string) (time.Time, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return time.Time{}, fmt.Errorf("failed to decode PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return cert.NotAfter, nil
}
