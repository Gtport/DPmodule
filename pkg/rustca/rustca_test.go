package rustca

import (
	"crypto/x509"
	"testing"
)

func TestEmbeddedCAParses(t *testing.T) {
	// Вшитый сертификат Минцифры обязан складываться в пул — иначе TLS к MAX
	// и ЛК РЖД не поднимется, и это должно падать здесь, а не в бою.
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("вшитый russian_trusted_ca.pem не разобран в пул сертификатов")
	}
}
