# Maintainer: Nk-YMZ <village_flute@outlook.com>

pkgname=molpe
pkgver=1.1.1
pkgrel=1
pkgdesc='Linux 终端中的网易云音乐 TUI 播放器'
arch=('x86_64')
url='https://github.com/Nk-YMZ/Molpe'
license=('MIT')
depends=('glibc' 'mpv')
makedepends=('git' 'go>=1.27.1')
source=("${pkgname}::git+${url}.git#tag=v${pkgver}")
b2sums=('SKIP')

prepare() {
	cd "$pkgname"
	export GOPATH="$srcdir/gopath"
	export GOCACHE="$srcdir/gocache"
	go mod download -modcacherw
}

build() {
	cd "$pkgname"
	export CGO_CPPFLAGS="$CPPFLAGS"
	export CGO_CFLAGS="$CFLAGS"
	export CGO_CXXFLAGS="$CXXFLAGS"
	export CGO_LDFLAGS="$LDFLAGS"
	export GOPATH="$srcdir/gopath"
	export GOCACHE="$srcdir/gocache"

	go build \
		-buildmode=pie \
		-trimpath \
		-mod=readonly \
		-modcacherw \
		-ldflags "-linkmode external -extldflags '${LDFLAGS}'" \
		-o molpe .
}

check() {
	cd "$pkgname"
	export GOPATH="$srcdir/gopath"
	export GOCACHE="$srcdir/gocache"
	go test -mod=readonly -modcacherw ./...
}

package() {
	install -Dm755 "$pkgname/molpe" "$pkgdir/usr/bin/molpe"
	install -Dm644 "$pkgname/LICENSE" "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
}
