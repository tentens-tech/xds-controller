// Copyright 2025 The Envoy XDS Controller Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sds

import (
	"context"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	xdserr "github.com/tentens-tech/xds-controller/pkg/xds/err"
	mock "github.com/tentens-tech/xds-controller/pkg/xds/mock"
	xdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types"
)

//nolint:gosec // G101: Test data, not actual credentials
var mockPriv = `
-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEAprf5of8ltgMjcL3ydqyZZsJXXFwjKlxxeRBXOeAiaDExvAjv
9v17zGSWlFtkHLGbO3+aQzzOBzhT3Pa0cnVdL0cgGsUqI6vKO5Frmlea1qEVSD8K
8LV+FNJe2HT/RxmkjSmfO1umaFUakCIj08Qvo8vMGj5fcI0UOlvoNEY4VeI/YZMj
JI4Uc2v4n8CIJnRI6BKYGwp190zDPSu95yMEX6z7qNKl96gCeSk7TXeup2K66y0w
8YrqHDLvCY0ogFbDDqFjqFdnZPbcoSxA9JJ/mWGXx8Po/7zGgGDb3RTOE9FxgJ/H
UbF4F70UDEhiIETDTY6QZsHSEm0ZebME1x072wIDAQABAoIBAD3LJQCxVGXxJdqs
3Mi10qnu0HiQQgx6dSidMOdntvkNetSqL19JtvAcPF/CvAmSnunfsurHB8pPS240
Fs/xxlc2sqSZfbP5AZ0wmkR7wg1ZaUz86O4tJw1KqBLs0o2k6IPV8IoMv1IecRkJ
PmRIbCv25rC3e6J4+A1lcVymxky2VN7pRMUypDFXwggAOoEoY0OAmzfhpeSxDwFT
xBc51jzvE5IeksZPlsLomE02y2q2t8CASDVg2UPkRPDrAsOwdwIBub/Dizfadslr
6noNJtdcKK4bGwU68t8tfRXQon9GKJg24wFj2R/sTpTYbQ5AEV1cQP/MOzmDRuGY
eR3qpjkCgYEAxVSOf0U2HAeKBvwbhsViEkDwm0mJBYW4KocdO+GqFaWc8RhjNE4X
izGZi4rC0KzYgc/5IG2SZdQXjARqqU3re5HRL+lAXBD1YsQUDXsIDwRawjCiTjAQ
k6E851iTARs51YgbiBFRPbnocm6YvkmXm7vG2pd/hKSyjpVJRvW9Pp0CgYEA2El3
umOc4UnqkkEdIf4yTMkh8BUPr1YNq5/X1ptF2H3/jZpEPwjn3+o0P76rSojK17Qf
SO8I4HfPlRab1ZkOCM7PANam4Q1w1WqHi6IDldpPLu+rscLpD0fPD8n+Kq8LhGxH
+Q9BdaZwXp5hr1cEgUA2k1C3WNhNmRQ0AcnwXtcCgYApUEe5O6tVePqb9cJpl/+t
ZK07Rc0LO/DP7pPfSqLKf275RyrV85eYS90iyv14pQd7Piihcm9ZJGt9pBsUsTyh
FWlfh40y+VX2xKiSHfUU98tsprQEfKmrzvEqWnAEpkeYfjONbFq++qJA+wi9pji4
oTrD3z4Sbkz37yd1VNO3PQKBgDbJfWuYeg/TYnkDx3Zp5qHuvQsMurlmafYUel8h
W/L4D7j13927ysi8kdmn2cn6lq9HMDmQW0ZI8ytH29eiepyejm8e8IzHk9JrtsQX
GSNndnFkQrC3t8OoI/pS53A2gQtdRmn/hExiCcreAc6hk0GOW4S7iIYX3KgvJvFh
DuNnAoGAEmqnxDHeMcSzSFUOAf+1xOl6B7Z1va9h5SZ3jcK5rC3aHjgx5TrumPnS
o2lDhuCwmJv1KUebxEbVAPmt7Ck7YaMkGQQjWAzasyBufEceeA5aAAIiFvxmnVul
nyFlsrzU2BUEOxfNjasii+TrwNorhc6OK2bGksx5FNTyFMweSM8=
-----END RSA PRIVATE KEY-----
`

var mockICA = `
-----BEGIN CERTIFICATE-----
MIIFWzCCA0OgAwIBAgIQTfQrldHumzpMLrM7jRBd1jANBgkqhkiG9w0BAQsFADBm
MQswCQYDVQQGEwJVUzEzMDEGA1UEChMqKFNUQUdJTkcpIEludGVybmV0IFNlY3Vy
aXR5IFJlc2VhcmNoIEdyb3VwMSIwIAYDVQQDExkoU1RBR0lORykgUHJldGVuZCBQ
ZWFyIFgxMB4XDTIwMDkwNDAwMDAwMFoXDTI1MDkxNTE2MDAwMFowWTELMAkGA1UE
BhMCVVMxIDAeBgNVBAoTFyhTVEFHSU5HKSBMZXQncyBFbmNyeXB0MSgwJgYDVQQD
Ex8oU1RBR0lORykgQXJ0aWZpY2lhbCBBcHJpY290IFIzMIIBIjANBgkqhkiG9w0B
AQEFAAOCAQ8AMIIBCgKCAQEAu6TR8+74b46mOE1FUwBrvxzEYLck3iasmKrcQkb+
gy/z9Jy7QNIAl0B9pVKp4YU76JwxF5DOZZhi7vK7SbCkK6FbHlyU5BiDYIxbbfvO
L/jVGqdsSjNaJQTg3C3XrJja/HA4WCFEMVoT2wDZm8ABC1N+IQe7Q6FEqc8NwmTS
nmmRQm4TQvr06DP+zgFK/MNubxWWDSbSKKTH5im5j2fZfg+j/tM1bGaczFWw8/lS
nukyn5J2L+NJYnclzkXoh9nMFnyPmVbfyDPOc4Y25aTzVoeBKXa/cZ5MM+WddjdL
biWvm19f1sYn1aRaAIrkppv7kkn83vcth8XCG39qC2ZvaQIDAQABo4IBEDCCAQww
DgYDVR0PAQH/BAQDAgGGMB0GA1UdJQQWMBQGCCsGAQUFBwMCBggrBgEFBQcDATAS
BgNVHRMBAf8ECDAGAQH/AgEAMB0GA1UdDgQWBBTecnpI3zHDplDfn4Uj31c3S10u
ZTAfBgNVHSMEGDAWgBS182Xy/rAKkh/7PH3zRKCsYyXDFDA2BggrBgEFBQcBAQQq
MCgwJgYIKwYBBQUHMAKGGmh0dHA6Ly9zdGcteDEuaS5sZW5jci5vcmcvMCsGA1Ud
HwQkMCIwIKAeoByGGmh0dHA6Ly9zdGcteDEuYy5sZW5jci5vcmcvMCIGA1UdIAQb
MBkwCAYGZ4EMAQIBMA0GCysGAQQBgt8TAQEBMA0GCSqGSIb3DQEBCwUAA4ICAQCN
DLam9yN0EFxxn/3p+ruWO6n/9goCAM5PT6cC6fkjMs4uas6UGXJjr5j7PoTQf3C1
vuxiIGRJC6qxV7yc6U0X+w0Mj85sHI5DnQVWN5+D1er7mp13JJA0xbAbHa3Rlczn
y2Q82XKui8WHuWra0gb2KLpfboYj1Ghgkhr3gau83pC/WQ8HfkwcvSwhIYqTqxoZ
Uq8HIf3M82qS9aKOZE0CEmSyR1zZqQxJUT7emOUapkUN9poJ9zGc+FgRZvdro0XB
yphWXDaqMYph0DxW/10ig5j4xmmNDjCRmqIKsKoWA52wBTKKXK1na2ty/lW5dhtA
xkz5rVZFd4sgS4J0O+zm6d5GRkWsNJ4knotGXl8vtS3X40KXeb3A5+/3p0qaD215
Xq8oSNORfB2oI1kQuyEAJ5xvPTdfwRlyRG3lFYodrRg6poUBD/8fNTXMtzydpRgy
zUQZh/18F6B/iW6cbiRN9r2Hkh05Om+q0/6w0DdZe+8YrNpfhSObr/1eVZbKGMIY
qKmyZbBNu5ysENIK5MPc14mUeKmFjpN840VR5zunoU52lqpLDua/qIM8idk86xGW
xx2ml43DO/Ya/tVZVok0mO0TUjzJIfPqyvr455IsIut4RlCR9Iq0EDTve2/ZwCuG
hSjpTUFGSiQrR2JK2Evp+o6AETUkBCO1aw0PpQBPDQ==
-----END CERTIFICATE-----

-----BEGIN CERTIFICATE-----
MIIFVDCCBDygAwIBAgIRAO1dW8lt+99NPs1qSY3Rs8cwDQYJKoZIhvcNAQELBQAw
cTELMAkGA1UEBhMCVVMxMzAxBgNVBAoTKihTVEFHSU5HKSBJbnRlcm5ldCBTZWN1
cml0eSBSZXNlYXJjaCBHcm91cDEtMCsGA1UEAxMkKFNUQUdJTkcpIERvY3RvcmVk
IER1cmlhbiBSb290IENBIFgzMB4XDTIxMDEyMDE5MTQwM1oXDTI0MDkzMDE4MTQw
M1owZjELMAkGA1UEBhMCVVMxMzAxBgNVBAoTKihTVEFHSU5HKSBJbnRlcm5ldCBT
ZWN1cml0eSBSZXNlYXJjaCBHcm91cDEiMCAGA1UEAxMZKFNUQUdJTkcpIFByZXRl
bmQgUGVhciBYMTCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBALbagEdD
Ta1QgGBWSYkyMhscZXENOBaVRTMX1hceJENgsL0Ma49D3MilI4KS38mtkmdF6cPW
nL++fgehT0FbRHZgjOEr8UAN4jH6omjrbTD++VZneTsMVaGamQmDdFl5g1gYaigk
kmx8OiCO68a4QXg4wSyn6iDipKP8utsE+x1E28SA75HOYqpdrk4HGxuULvlr03wZ
GTIf/oRt2/c+dYmDoaJhge+GOrLAEQByO7+8+vzOwpNAPEx6LW+crEEZ7eBXih6V
P19sTGy3yfqK5tPtTdXXCOQMKAp+gCj/VByhmIr+0iNDC540gtvV303WpcbwnkkL
YC0Ft2cYUyHtkstOfRcRO+K2cZozoSwVPyB8/J9RpcRK3jgnX9lujfwA/pAbP0J2
UPQFxmWFRQnFjaq6rkqbNEBgLy+kFL1NEsRbvFbKrRi5bYy2lNms2NJPZvdNQbT/
2dBZKmJqxHkxCuOQFjhJQNeO+Njm1Z1iATS/3rts2yZlqXKsxQUzN6vNbD8KnXRM
EeOXUYvbV4lqfCf8mS14WEbSiMy87GB5S9ucSV1XUrlTG5UGcMSZOBcEUpisRPEm
QWUOTWIoDQ5FOia/GI+Ki523r2ruEmbmG37EBSBXdxIdndqrjy+QVAmCebyDx9eV
EGOIpn26bW5LKerumJxa/CFBaKi4bRvmdJRLAgMBAAGjgfEwge4wDgYDVR0PAQH/
BAQDAgEGMA8GA1UdEwEB/wQFMAMBAf8wHQYDVR0OBBYEFLXzZfL+sAqSH/s8ffNE
oKxjJcMUMB8GA1UdIwQYMBaAFAhX2onHolN5DE/d4JCPdLriJ3NEMDgGCCsGAQUF
BwEBBCwwKjAoBggrBgEFBQcwAoYcaHR0cDovL3N0Zy1kc3QzLmkubGVuY3Iub3Jn
LzAtBgNVHR8EJjAkMCKgIKAehhxodHRwOi8vc3RnLWRzdDMuYy5sZW5jci5vcmcv
MCIGA1UdIAQbMBkwCAYGZ4EMAQIBMA0GCysGAQQBgt8TAQEBMA0GCSqGSIb3DQEB
CwUAA4IBAQB7tR8B0eIQSS6MhP5kuvGth+dN02DsIhr0yJtk2ehIcPIqSxRRmHGl
4u2c3QlvEpeRDp2w7eQdRTlI/WnNhY4JOofpMf2zwABgBWtAu0VooQcZZTpQruig
F/z6xYkBk3UHkjeqxzMN3d1EqGusxJoqgdTouZ5X5QTTIee9nQ3LEhWnRSXDx7Y0
ttR1BGfcdqHopO4IBqAhbkKRjF5zj7OD8cG35omywUbZtOJnftiI0nFcRaxbXo0v
oDfLD0S6+AC2R3tKpqjkNX6/91hrRFglUakyMcZU/xleqbv6+Lr3YD8PsBTub6lI
oZ2lS38fL18Aon458fbc0BPHtenfhKj5
-----END CERTIFICATE-----

`

var mockPub99Production = `
-----BEGIN CERTIFICATE-----
MIIDyTCCArGgAwIBAgIUXPEUTvkMDmZm0G92JzwDOnudXIAwDQYJKoZIhvcNAQEL
BQAwVDEXMBUGA1UEAwwOZmFrZWRvbWFpbi5jb20xCzAJBgNVBAYTAlVTMRUwEwYD
VQQKDAxMZXRzIEVuY3J5cHQxFTATBgNVBAsMDExldHMgRW5jcnlwdDAgFw0yMjEw
MzEyMzM3NTRaGA8yMTIxMTAwNzIzMzc1NFowVDEXMBUGA1UEAwwOZmFrZWRvbWFp
bi5jb20xCzAJBgNVBAYTAlVTMRUwEwYDVQQKDAxMZXRzIEVuY3J5cHQxFTATBgNV
BAsMDExldHMgRW5jcnlwdDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEB
AKa3+aH/JbYDI3C98nasmWbCV1xcIypccXkQVzngImgxMbwI7/b9e8xklpRbZByx
mzt/mkM8zgc4U9z2tHJ1XS9HIBrFKiOryjuRa5pXmtahFUg/CvC1fhTSXth0/0cZ
pI0pnztbpmhVGpAiI9PEL6PLzBo+X3CNFDpb6DRGOFXiP2GTIySOFHNr+J/AiCZ0
SOgSmBsKdfdMwz0rvecjBF+s+6jSpfeoAnkpO013rqdiuustMPGK6hwy7wmNKIBW
ww6hY6hXZ2T23KEsQPSSf5lhl8fD6P+8xoBg290UzhPRcYCfx1GxeBe9FAxIYiBE
w02OkGbB0hJtGXmzBNcdO9sCAwEAAaOBkDCBjTAdBgNVHQ4EFgQUPNsMlJJ6emlK
0/R8I2ru+L1iWSAwHwYDVR0jBBgwFoAUPNsMlJJ6emlK0/R8I2ru+L1iWSAwDgYD
VR0PAQH/BAQDAgWgMCAGA1UdJQEB/wQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjAZ
BgNVHREEEjAQgg5mYWtlZG9tYWluLmNvbTANBgkqhkiG9w0BAQsFAAOCAQEAQCaE
fVXzyOWkxRzsPJKGcdIwZxzhaepNR302C5pXDC2e4gbv3Cv9O7SrSYGMQUbnn5N0
qnUjghrOjpuhn9k1vjzZuiYVpCHBoF0elpM77vqe1QHzuLVrzXg/Axs+41xZ8gdF
9YEc/NODavdXqo9Vb3Kf6ZA4jyn/u+VNHd/8ISkmbhWcetYawyrQbzbaNiYPF2+K
30v1xZXsQqz8Zy9jReZIwTFMT4Ik1e/rFDWCYfkhHU+hfOq8u4Ym4JE/1zm7mu+M
fdzB1OF++frAwnCHLXT5K9qQ6+2JRMo3uPBOkEBaSWulmo9ARcUczls/MjPl/vhD
T6bsJ2W8DefuWVth7A==
-----END CERTIFICATE-----
` + mockICA

var mockPub99Staging = `
-----BEGIN CERTIFICATE-----
MIIEFTCCAv2gAwIBAgIUf8zDh6pzyQlgxSzfDaXfn6Bcmi4wDQYJKoZIhvcNAQEL
BQAwejEXMBUGA1UEAwwOZmFrZWRvbWFpbi5jb20xEDAOBgNVBAMMB1NUQUdJTkcx
CzAJBgNVBAYTAlVTMR8wHQYDVQQKDBYoU1RBR0lORykgTGV0cyBFbmNyeXB0MR8w
HQYDVQQLDBYoU1RBR0lORykgTGV0cyBFbmNyeXB0MCAXDTIyMTEwMTAwMzIwMloY
DzIxMjExMDA4MDAzMjAyWjB6MRcwFQYDVQQDDA5mYWtlZG9tYWluLmNvbTEQMA4G
A1UEAwwHU1RBR0lORzELMAkGA1UEBhMCVVMxHzAdBgNVBAoMFihTVEFHSU5HKSBM
ZXRzIEVuY3J5cHQxHzAdBgNVBAsMFihTVEFHSU5HKSBMZXRzIEVuY3J5cHQwggEi
MA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCmt/mh/yW2AyNwvfJ2rJlmwldc
XCMqXHF5EFc54CJoMTG8CO/2/XvMZJaUW2QcsZs7f5pDPM4HOFPc9rRydV0vRyAa
xSojq8o7kWuaV5rWoRVIPwrwtX4U0l7YdP9HGaSNKZ87W6ZoVRqQIiPTxC+jy8wa
Pl9wjRQ6W+g0RjhV4j9hkyMkjhRza/ifwIgmdEjoEpgbCnX3TMM9K73nIwRfrPuo
0qX3qAJ5KTtNd66nYrrrLTDxiuocMu8JjSiAVsMOoWOoV2dk9tyhLED0kn+ZYZfH
w+j/vMaAYNvdFM4T0XGAn8dRsXgXvRQMSGIgRMNNjpBmwdISbRl5swTXHTvbAgMB
AAGjgZAwgY0wHQYDVR0OBBYEFDzbDJSSenppStP0fCNq7vi9YlkgMB8GA1UdIwQY
MBaAFDzbDJSSenppStP0fCNq7vi9YlkgMA4GA1UdDwEB/wQEAwIFoDAgBgNVHSUB
Af8EFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwGQYDVR0RBBIwEIIOZmFrZWRvbWFp
bi5jb20wDQYJKoZIhvcNAQELBQADggEBAChbnaqjQOV9J7wAMsHD9mm/vrl/Yafk
36Y9VKBXxSF73Fr2L1rvN4BmMn1pyxezeKym825xBF0uFIft8Sl3S5V023Rac8bm
5LTkY6GZ4pljRH5ww+YoCuWgeCIvakBwfzTfXUVYdVavVGwh8yO+prf3QWSkKcot
QWZpyFlTWKtryJD7rN4VNzxPguhyksK/GH0dbJb6FfutiRVhL+HqUsXTk1lRLtss
28WtYGHInPnbruAu0+M1caC2g3XV+aH6zcCNVX4l4PGCqi9ut71c02pdZSS5m6Ts
wFcZ9GqXRuHaESAeTzMwkYEPLNya4CsSUmT5Ds6xzHFo/6UbpLHEI8w=
-----END CERTIFICATE-----
` + mockICA

var mockPub1dStaging = `
-----BEGIN CERTIFICATE-----
MIIEFTCCAv2gAwIBAgIUf8zDh6pzyQlgxSzfDaXfn6Bcmi4wDQYJKoZIhvcNAQEL
BQAwejEXMBUGA1UEAwwOZmFrZWRvbWFpbi5jb20xEDAOBgNVBAMMB1NUQUdJTkcx
CzAJBgNVBAYTAlVTMR8wHQYDVQQKDBYoU1RBR0lORykgTGV0cyBFbmNyeXB0MR8w
HQYDVQQLDBYoU1RBR0lORykgTGV0cyBFbmNyeXB0MCAXDTIyMTEwMTAwMzIwMloY
DzIxMjExMDA4MDAzMjAyWjB6MRcwFQYDVQQDDA5mYWtlZG9tYWluLmNvbTEQMA4G
A1UEAwwHU1RBR0lORzELMAkGA1UEBhMCVVMxHzAdBgNVBAoMFihTVEFHSU5HKSBM
ZXRzIEVuY3J5cHQxHzAdBgNVBAsMFihTVEFHSU5HKSBMZXRzIEVuY3J5cHQwggEi
MA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCmt/mh/yW2AyNwvfJ2rJlmwldc
XCMqXHF5EFc54CJoMTG8CO/2/XvMZJaUW2QcsZs7f5pDPM4HOFPc9rRydV0vRyAa
xSojq8o7kWuaV5rWoRVIPwrwtX4U0l7YdP9HGaSNKZ87W6ZoVRqQIiPTxC+jy8wa
Pl9wjRQ6W+g0RjhV4j9hkyMkjhRza/ifwIgmdEjoEpgbCnX3TMM9K73nIwRfrPuo
0qX3qAJ5KTtNd66nYrrrLTDxiuocMu8JjSiAVsMOoWOoV2dk9tyhLED0kn+ZYZfH
w+j/vMaAYNvdFM4T0XGAn8dRsXgXvRQMSGIgRMNNjpBmwdISbRl5swTXHTvbAgMB
AAGjgZAwgY0wHQYDVR0OBBYEFDzbDJSSenppStP0fCNq7vi9YlkgMB8GA1UdIwQY
MBaAFDzbDJSSenppStP0fCNq7vi9YlkgMA4GA1UdDwEB/wQEAwIFoDAgBgNVHSUB
Af8EFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwGQYDVR0RBBIwEIIOZmFrZWRvbWFp
bi5jb20wDQYJKoZIhvcNAQELBQADggEBAChbnaqjQOV9J7wAMsHD9mm/vrl/Yafk
36Y9VKBXxSF73Fr2L1rvN4BmMn1pyxezeKym825xBF0uFIft8Sl3S5V023Rac8bm
5LTkY6GZ4pljRH5ww+YoCuWgeCIvakBwfzTfXUVYdVavVGwh8yO+prf3QWSkKcot
QWZpyFlTWKtryJD7rN4VNzxPguhyksK/GH0dbJb6FfutiRVhL+HqUsXTk1lRLtss
28WtYGHInPnbruAu0+M1caC2g3XV+aH6zcCNVX4l4PGCqi9ut71c02pdZSS5m6Ts
wFcZ9GqXRuHaESAeTzMwkYEPLNya4CsSUmT5Ds6xzHFo/6UbpLHEI8w=
-----END CERTIFICATE-----
` + mockICA

var mockDomainConfigManual = &xdstypes.DomainConfig{
	SecretName: "fake",
	Domains: []string{
		"fakedomain.com",
	},
	Challenge: &xdstypes.ChallengeConfig{
		ChallengeType: xdstypes.MANUAL,
	},
	Config: xdstypes.StorageConfig{
		Type: xdstypes.Vault,
	},
}

var mockDomainConfigStaging = &xdstypes.DomainConfig{
	SecretName: "fakeStaging",
	Domains: []string{
		"fakedomain.com",
	},
	Challenge: &xdstypes.ChallengeConfig{
		ChallengeType: xdstypes.DNS01,
		ACMEEnv:       xdstypes.Staging,
	},
	Config: xdstypes.StorageConfig{
		Type: xdstypes.Vault,
	},
}

var mockMultiDomainConfigStaging = &xdstypes.DomainConfig{
	SecretName: "fakeStaging",
	Domains: []string{
		"fakedomain.com",
		"fakedomain2.com",
		"fakedomain3.com",
		"fakedomain4.com",
	},
	Challenge: &xdstypes.ChallengeConfig{
		ChallengeType: xdstypes.DNS01,
		ACMEEnv:       xdstypes.Staging,
	},
	Config: xdstypes.StorageConfig{
		Type: xdstypes.Vault,
	},
}

var mockDomainConfigProduction = &xdstypes.DomainConfig{
	SecretName: "fakeProduction",
	Domains: []string{
		"fakedomain.com",
	},
	Challenge: &xdstypes.ChallengeConfig{
		ChallengeType: xdstypes.DNS01,
		ACMEEnv:       xdstypes.Production,
	},
	Config: xdstypes.StorageConfig{
		Type: xdstypes.Vault,
	},
}

var mockCert1dStaging = xdstypes.Cert{Pub: []byte(mockPub1dStaging), Priv: []byte(mockPriv)}
var mockCert99Staging = xdstypes.Cert{Pub: []byte(mockPub99Staging), Priv: []byte(mockPriv)}
var mockCert99Production = xdstypes.Cert{Pub: []byte(mockPub99Production), Priv: []byte(mockPriv)}

func TestCertNotFoundGeneratingError_makeSecrets(t *testing.T) {
	ctl := gomock.NewController(t)
	defer ctl.Finish()

	config := mock.NewMockGlobalConfig(ctl)
	config.EXPECT().GetDomainConfigs().Return(map[string][]*xdstypes.DomainConfig{
		"node": {mockDomainConfigStaging},
	}).AnyTimes()
	config.EXPECT().GetDryRun().Return(false).AnyTimes()
	config.EXPECT().GetRenewBeforeExpireInMinutes().Return(1440).AnyTimes()
	config.EXPECT().StorageConfigRead(gomock.Any(), mockDomainConfigStaging).Return(xdstypes.Cert{}, xdserr.ErrCertNotFound).AnyTimes()
	config.EXPECT().CheckVaultWriteAccess(mockDomainConfigStaging).Return(nil).AnyTimes()

	le := mock.NewMockCertGetter(ctl)
	le.EXPECT().Get(mockDomainConfigStaging).Return(xdstypes.Cert{}, xdserr.ErrGetCert).AnyTimes()

	_, _, _, err := makeSecret(context.Background(), mockDomainConfigStaging, config, le, true)
	require.EqualError(t, err, fmt.Errorf(xdserr.ErrGetCert.Error()+": %w", xdserr.ErrGetCert).Error())
}

func TestCertNotFoundGenerating_makeSecrets(t *testing.T) {
	ctl := gomock.NewController(t)
	defer ctl.Finish()

	config := mock.NewMockGlobalConfig(ctl)
	config.EXPECT().GetDomainConfigs().Return(map[string][]*xdstypes.DomainConfig{
		"node": {mockDomainConfigStaging},
	}).AnyTimes()
	config.EXPECT().GetDryRun().Return(false).AnyTimes()
	config.EXPECT().GetRenewBeforeExpireInMinutes().Return(1440).AnyTimes()
	config.EXPECT().StorageConfigRead(gomock.Any(), mockDomainConfigStaging).Return(xdstypes.Cert{}, xdserr.ErrCertNotFound).AnyTimes()
	config.EXPECT().CheckVaultWriteAccess(mockDomainConfigStaging).Return(nil).AnyTimes()
	config.EXPECT().StorageConfigWrite(mockDomainConfigStaging, mockCert99Staging).Return(nil).AnyTimes()

	le := mock.NewMockCertGetter(ctl)
	le.EXPECT().Get(mockDomainConfigStaging).Return(mockCert99Staging, nil).AnyTimes()

	want := &auth.Secret{
		Name: mockMultiDomainConfigStaging.SecretName,
		Type: &auth.Secret_TlsCertificate{
			TlsCertificate: &auth.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPub99Staging)},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPriv)},
				},
			},
		},
	}
	resp, _, expireTime, err := makeSecret(context.Background(), mockDomainConfigStaging, config, le, true)
	require.NoError(t, err)
	require.Equal(t, want, resp)
	require.False(t, expireTime.IsZero()) // Ensure we got an expiration time
}

func TestCertIsManualAndNotFoundNoError_makeSecrets(t *testing.T) {
	ctl := gomock.NewController(t)
	defer ctl.Finish()

	config := mock.NewMockGlobalConfig(ctl)
	config.EXPECT().GetDomainConfigs().Return(map[string][]*xdstypes.DomainConfig{
		"node": {mockDomainConfigManual},
	}).AnyTimes()
	config.EXPECT().GetDryRun().Return(false).AnyTimes()
	config.EXPECT().GetRenewBeforeExpireInMinutes().Return(1440).AnyTimes()
	config.EXPECT().StorageConfigRead(gomock.Any(), mockDomainConfigManual).Return(xdstypes.Cert{}, xdserr.ErrCertNotFound).AnyTimes()

	le := mock.NewMockCertGetter(ctl)

	resp, _, expireTime, err := makeSecret(context.Background(), mockDomainConfigManual, config, le, true)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, expireTime.IsZero()) // Manual certs should have zero expiration time
}

func TestCertFromOneDomainToMulti_makeSecrets(t *testing.T) {
	ctl := gomock.NewController(t)
	defer ctl.Finish()

	config := mock.NewMockGlobalConfig(ctl)
	config.EXPECT().GetDomainConfigs().Return(map[string][]*xdstypes.DomainConfig{
		"node": {mockMultiDomainConfigStaging},
	}).AnyTimes()
	config.EXPECT().GetDryRun().Return(false).AnyTimes()
	config.EXPECT().GetRenewBeforeExpireInMinutes().Return(1440).AnyTimes()
	config.EXPECT().StorageConfigRead(gomock.Any(), mockMultiDomainConfigStaging).Return(mockCert99Staging, nil).AnyTimes()
	// Check vault write access before expensive LE operations (forceRegenerate due to domain count change)
	config.EXPECT().CheckVaultWriteAccess(mockMultiDomainConfigStaging).Return(nil).AnyTimes()
	// Write new cert after successful generation
	config.EXPECT().StorageConfigWrite(mockMultiDomainConfigStaging, mockCert99Staging).Return(nil).AnyTimes()

	le := mock.NewMockCertGetter(ctl)
	le.EXPECT().Get(mockMultiDomainConfigStaging).Return(mockCert99Staging, nil).AnyTimes()

	want := &auth.Secret{
		Name: mockMultiDomainConfigStaging.SecretName,
		Type: &auth.Secret_TlsCertificate{
			TlsCertificate: &auth.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPub99Staging)},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPriv)},
				},
			},
		},
	}
	resp, _, expireTime, err := makeSecret(context.Background(), mockMultiDomainConfigStaging, config, le, true)
	require.NoError(t, err)
	require.Equal(t, want, resp)
	require.False(t, expireTime.IsZero()) // Ensure we got an expiration time
}

func TestCertStaging_makeSecrets(t *testing.T) {
	ctl := gomock.NewController(t)
	defer ctl.Finish()

	le := mock.NewMockCertGetter(ctl)
	config := mock.NewMockGlobalConfig(ctl)

	config.EXPECT().GetDomainConfigs().Return(map[string][]*xdstypes.DomainConfig{
		"node": {mockDomainConfigStaging},
	}).AnyTimes()
	config.EXPECT().GetDryRun().Return(false).AnyTimes()
	config.EXPECT().GetRenewBeforeExpireInMinutes().Return(1440).AnyTimes()
	config.EXPECT().StorageConfigRead(gomock.Any(), mockDomainConfigStaging).Return(mockCert99Staging, nil).AnyTimes()

	want := &auth.Secret{
		Name: mockDomainConfigStaging.SecretName,
		Type: &auth.Secret_TlsCertificate{
			TlsCertificate: &auth.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPub99Staging)},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPriv)},
				},
			},
		},
	}
	resp, _, expireTime, err := makeSecret(context.Background(), mockDomainConfigStaging, config, le, true)
	require.NoError(t, err)
	require.Equal(t, want, resp)
	require.False(t, expireTime.IsZero()) // Ensure we got an expiration time
}

func TestCertProduction_makeSecrets(t *testing.T) {
	ctl := gomock.NewController(t)
	defer ctl.Finish()

	le := mock.NewMockCertGetter(ctl)
	config := mock.NewMockGlobalConfig(ctl)

	config.EXPECT().GetDomainConfigs().Return(map[string][]*xdstypes.DomainConfig{
		"node": {mockDomainConfigProduction},
	}).AnyTimes()
	config.EXPECT().GetDryRun().Return(false).AnyTimes()
	config.EXPECT().GetRenewBeforeExpireInMinutes().Return(1440).AnyTimes()
	config.EXPECT().StorageConfigRead(gomock.Any(), mockDomainConfigProduction).Return(mockCert99Production, nil).AnyTimes()

	want := &auth.Secret{
		Name: mockDomainConfigProduction.SecretName,
		Type: &auth.Secret_TlsCertificate{
			TlsCertificate: &auth.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPub99Production)},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPriv)},
				},
			},
		},
	}
	resp, _, expireTime, err := makeSecret(context.Background(), mockDomainConfigProduction, config, le, true)
	require.NoError(t, err)
	require.Equal(t, want, resp)
	require.False(t, expireTime.IsZero()) // Ensure we got an expiration time
}

func TestCertChangedEnvStageToProd_makeSecrets(t *testing.T) {
	ctl := gomock.NewController(t)
	defer ctl.Finish()

	le := mock.NewMockCertGetter(ctl)
	le.EXPECT().Get(mockDomainConfigProduction).Return(mockCert99Production, nil).AnyTimes()

	config := mock.NewMockGlobalConfig(ctl)

	config.EXPECT().GetDomainConfigs().Return(map[string][]*xdstypes.DomainConfig{
		"node": {mockDomainConfigProduction},
	}).AnyTimes()
	config.EXPECT().GetDryRun().Return(false).AnyTimes()
	config.EXPECT().GetRenewBeforeExpireInMinutes().Return(1440).AnyTimes()
	config.EXPECT().StorageConfigRead(gomock.Any(), mockDomainConfigProduction).Return(mockCert99Staging, nil).AnyTimes()
	// Check vault write access before expensive LE operations (forceRegenerate due to env change)
	config.EXPECT().CheckVaultWriteAccess(mockDomainConfigProduction).Return(nil).AnyTimes()
	// Write new cert after successful generation
	config.EXPECT().StorageConfigWrite(mockDomainConfigProduction, mockCert99Production).Return(nil).AnyTimes()

	want := &auth.Secret{
		Name: mockDomainConfigProduction.SecretName,
		Type: &auth.Secret_TlsCertificate{
			TlsCertificate: &auth.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPub99Production)},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPriv)},
				},
			},
		},
	}
	resp, _, expireTime, err := makeSecret(context.Background(), mockDomainConfigProduction, config, le, true)
	require.NoError(t, err)
	require.Equal(t, want, resp)
	require.False(t, expireTime.IsZero()) // Ensure we got an expiration time
}

func TestCertChangedEnvProdToStage_makeSecrets(t *testing.T) {
	ctl := gomock.NewController(t)
	defer ctl.Finish()

	config := mock.NewMockGlobalConfig(ctl)

	config.EXPECT().GetDomainConfigs().Return(map[string][]*xdstypes.DomainConfig{
		"node": {mockDomainConfigStaging},
	}).AnyTimes()
	config.EXPECT().GetDryRun().Return(false).AnyTimes()
	config.EXPECT().GetRenewBeforeExpireInMinutes().Return(1440).AnyTimes()
	config.EXPECT().StorageConfigRead(gomock.Any(), mockDomainConfigStaging).Return(mockCert99Production, nil).AnyTimes()

	le := mock.NewMockCertGetter(ctl)
	le.EXPECT().Get(mockDomainConfigStaging).Return(mockCert99Staging, nil).AnyTimes()

	// Check vault write access before expensive LE operations (forceRegenerate due to env change)
	config.EXPECT().CheckVaultWriteAccess(mockDomainConfigStaging).Return(nil).AnyTimes()
	// Write new cert after successful generation
	config.EXPECT().StorageConfigWrite(mockDomainConfigStaging, mockCert99Staging).Return(nil).AnyTimes()

	want := &auth.Secret{
		Name: mockDomainConfigStaging.SecretName,
		Type: &auth.Secret_TlsCertificate{
			TlsCertificate: &auth.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPub99Staging)},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPriv)},
				},
			},
		},
	}
	resp, _, expireTime, err := makeSecret(context.Background(), mockDomainConfigStaging, config, le, true)
	require.NoError(t, err)
	require.Equal(t, want, resp)
	require.False(t, expireTime.IsZero()) // Ensure we got an expiration time
}

func TestCertNotFound_makeSecrets(t *testing.T) {
	ctl := gomock.NewController(t)
	defer ctl.Finish()

	le := mock.NewMockCertGetter(ctl)
	le.EXPECT().Get(mockDomainConfigStaging).Return(mockCert99Staging, nil).AnyTimes()

	config := mock.NewMockGlobalConfig(ctl)
	config.EXPECT().GetDomainConfigs().Return(map[string][]*xdstypes.DomainConfig{
		"node": {mockDomainConfigStaging},
	}).AnyTimes()
	config.EXPECT().GetDryRun().Return(false).AnyTimes()
	config.EXPECT().GetRenewBeforeExpireInMinutes().Return(1440).AnyTimes()
	config.EXPECT().StorageConfigRead(gomock.Any(), mockDomainConfigStaging).Return(xdstypes.Cert{}, xdserr.ErrCertNotFound).AnyTimes()
	// Check vault write access before expensive LE operations
	config.EXPECT().CheckVaultWriteAccess(mockDomainConfigStaging).Return(nil).AnyTimes()
	// Write actual cert after successful generation
	config.EXPECT().StorageConfigWrite(mockDomainConfigStaging, mockCert99Staging).Return(nil).AnyTimes()

	want := &auth.Secret{
		Name: mockDomainConfigStaging.SecretName,
		Type: &auth.Secret_TlsCertificate{
			TlsCertificate: &auth.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPub99Staging)},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPriv)},
				},
			},
		},
	}
	resp, _, expireTime, err := makeSecret(context.Background(), mockDomainConfigStaging, config, le, true)
	require.NoError(t, err)
	require.Equal(t, want, resp)
	require.False(t, expireTime.IsZero()) // Ensure we got an expiration time
}

func TestCertRenew_makeSecrets(t *testing.T) {
	ctl := gomock.NewController(t)
	defer ctl.Finish()

	config := mock.NewMockGlobalConfig(ctl)
	config.EXPECT().GetDomainConfigs().Return(map[string][]*xdstypes.DomainConfig{
		"node": {mockDomainConfigStaging},
	}).AnyTimes()
	config.EXPECT().GetDryRun().Return(false).AnyTimes()
	config.EXPECT().GetRenewBeforeExpireInMinutes().Return(1440).AnyTimes()
	config.EXPECT().StorageConfigRead(gomock.Any(), mockDomainConfigStaging).Return(mockCert1dStaging, nil).AnyTimes()

	le := mock.NewMockCertGetter(ctl)
	le.EXPECT().Get(mockDomainConfigStaging).Return(mockCert99Staging, nil).AnyTimes()

	// Check vault write access before expensive LE operations (renewal due to expiry)
	config.EXPECT().CheckVaultWriteAccess(mockDomainConfigStaging).Return(nil).AnyTimes()
	// Write new cert after successful generation
	config.EXPECT().StorageConfigWrite(mockDomainConfigStaging, mockCert99Staging).Return(nil).AnyTimes()

	want := &auth.Secret{
		Name: mockDomainConfigStaging.SecretName,
		Type: &auth.Secret_TlsCertificate{
			TlsCertificate: &auth.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPub99Staging)},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(mockPriv)},
				},
			},
		},
	}
	resp, _, expireTime, err := makeSecret(context.Background(), mockDomainConfigStaging, config, le, true)
	require.NoError(t, err)
	require.Equal(t, want, resp)
	require.False(t, expireTime.IsZero()) // Ensure we got an expiration time
}
