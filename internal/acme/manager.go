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
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/alidns"
	"github.com/go-acme/lego/v4/registration"
	"go.uber.org/zap"

	"github.com/solarhell/certship/pkg/ent"
)

// user 实现 registration.User 接口
type user struct {
	Email        string
	Registration *registration.Resource
	key          crypto.PrivateKey
}

func (u *user) GetEmail() string                        { return u.Email }
func (u *user) GetRegistration() *registration.Resource { return u.Registration }
func (u *user) GetPrivateKey() crypto.PrivateKey        { return u.key }

// CertResult 保存申请结果，AccountKeyPEM 和 RegistrationJSON 应持久化回数据库
type CertResult struct {
	CertPEM          string
	KeyPEM           string
	AccountKeyPEM    string // 账号私钥 PEM（每次均返回，供调用方持久化）
	RegistrationJSON string // 注册信息 JSON（每次均返回）
}

type Manager struct {
	cfg    *ent.AppSettings
	logger *zap.Logger
}

func NewManager(cfg *ent.AppSettings, logger *zap.Logger) *Manager {
	return &Manager{cfg: cfg, logger: logger}
}

// ObtainCert 使用阿里云 DNS-01 挑战申请证书
// accountKeyPEM / registrationJSON 从数据库传入（首次为空），返回的 AccountKeyPEM / RegistrationJSON 应持久化回数据库
func (m *Manager) ObtainCert(domain, accessKeyID, accessKeySecret, accountKeyPEM, registrationJSON string) (*CertResult, error) {
	u, err := m.buildUser(accountKeyPEM, registrationJSON)
	if err != nil {
		return nil, fmt.Errorf("build acme user: %w", err)
	}

	legoCfg := lego.NewConfig(u)
	legoCfg.CADirURL = m.cfg.AcmeDirectoryURL
	legoCfg.Certificate.KeyType = certcrypto.RSA2048

	client, err := lego.NewClient(legoCfg)
	if err != nil {
		return nil, fmt.Errorf("create lego client: %w", err)
	}

	dnsCfg := alidns.NewDefaultConfig()
	dnsCfg.APIKey = accessKeyID
	dnsCfg.SecretKey = accessKeySecret
	provider, err := alidns.NewDNSProviderConfig(dnsCfg)
	if err != nil {
		return nil, fmt.Errorf("create alidns provider: %w", err)
	}
	if err := client.Challenge.SetDNS01Provider(provider); err != nil {
		return nil, fmt.Errorf("set dns01 provider: %w", err)
	}

	if u.Registration == nil {
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return nil, fmt.Errorf("register acme account: %w", err)
		}
		u.Registration = reg
		m.logger.Info("ACME 账号注册成功")
	}

	m.logger.Info("开始申请证书", zap.String("domain", domain))
	certs, err := client.Certificate.Obtain(certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("obtain cert for %s: %w", domain, err)
	}
	m.logger.Info("证书申请成功", zap.String("domain", domain))

	keyPEMBytes := pem.EncodeToMemory(certcrypto.PEMBlock(u.key))
	regJSON, _ := json.Marshal(u.Registration)

	return &CertResult{
		CertPEM:          string(certs.Certificate),
		KeyPEM:           string(certs.PrivateKey),
		AccountKeyPEM:    string(keyPEMBytes),
		RegistrationJSON: string(regJSON),
	}, nil
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

	u := &user{Email: m.cfg.AcmeEmail, key: privateKey}

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
