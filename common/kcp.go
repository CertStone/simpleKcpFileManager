package common

import (
	"crypto/sha1"
	"net"

	"github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
	"golang.org/x/crypto/pbkdf2"
)

// 定义默认加密密钥和盐值
const (
	Salt       = "kcp-file-transfer"
	DefaultKey = "your-secret-key"
)

// IsDefaultKey 检查是否使用默认密钥
func IsDefaultKey(key string) bool {
	return key == "" || key == DefaultKey
}

// GetEffectiveKey 返回实际使用的密钥
func GetEffectiveKey(key string) string {
	if key == "" {
		return DefaultKey
	}
	return key
}

// 生成加密块
func GetBlockCrypt(key string) (kcp.BlockCrypt, error) {
	effectiveKey := GetEffectiveKey(key)
	pass := pbkdf2.Key([]byte(effectiveKey), []byte(Salt), 4096, 32, sha1.New)
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
