// Package rustca — корневой сертификат Минцифры (цепочка Russian Trusted
// Root CA + Sub CA), вшитый в бинарь. Общий якорь TLS для внешних сервисов,
// перешедших на нацвендора: platform-api.max.ru (MAX, с самого начала) и
// cargolk.rzd.ru (ЛК РЖД, сертификат от 23.06.2026). Полагаться на системное
// хранилище ОС нельзя: сило — свой процесс несёт свой якорь, а не ждёт, что
// сертификат доставят на каждую машину руками.
package rustca

import (
	"crypto/x509"
	_ "embed"
	"errors"
)

//go:embed russian_trusted_ca.pem
var caPEM []byte

// Pool возвращает пул TLS: системные сертификаты + вшитый корень Минцифры.
// Системный пул недоступен → пул из одного якоря (нацвендорным хостам хватает).
func Pool() (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("вшитый russian_trusted_ca.pem не разобран в пул сертификатов")
	}
	return pool, nil
}
