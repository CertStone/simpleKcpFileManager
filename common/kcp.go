package common

import (
	"crypto/sha256"
	"fmt"
	"net"

	"github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
	"golang.org/x/crypto/pbkdf2"
)

// 定义加密盐值
const (
	Salt = "kcp-file-transfer"
)

// hashKey 对输入密钥进行 SHA-256 哈希以提高安全性
// 这样即使用户输入短密钥也能保证足够的密钥强度
func hashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", hash)
}

// ValidateKey 验证密钥是否有效
func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("encryption key is required")
	}
	return nil
}

// 生成加密块
func GetBlockCrypt(key string) (kcp.BlockCrypt, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	// 先对输入密钥进行哈希，提高短密钥的安全性
	hashedKey := hashKey(key)
	// 使用 PBKDF2 从哈希后的密钥派生最终密钥
	pass := pbkdf2.Key([]byte(hashedKey), []byte(Salt), 4096, 32, sha256.New)
	return kcp.NewAESBlockCrypt(pass)
}

// 配置 KCP 连接参数 (参考 kcptun fast3 模式)
func ConfigKCP(sess *kcp.UDPSession) {
	sess.SetWindowSize(1024, 1024)
	sess.SetNoDelay(1, 10, 2, 1)
	sess.SetACKNoDelay(true)
	sess.SetMtu(1350)
}

// Smux 配置
func SmuxConfig() *smux.Config {
	return smux.DefaultConfig()
}

// 适配器：将 smux.Session 适配为 net.Listener 接口，以便 http.Serve 使用
type SmuxListener struct {
	Session *smux.Session
}

func (l *SmuxListener) Accept() (net.Conn, error) {
	return l.Session.AcceptStream()
}

func (l *SmuxListener) Close() error {
	return l.Session.Close()
}

func (l *SmuxListener) Addr() net.Addr {
	return l.Session.LocalAddr()
}
