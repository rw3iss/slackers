# Slackers Packaging Plan

Guide for publishing Slackers to package managers across all major platforms.

## Prerequisites

- GitHub releases with binaries for all platforms (already done)
- Repository: `github.com/rw3iss/slackers`
- Current release binaries: `slackers-{linux,darwin}-{amd64,arm64}`, `slackers-windows-amd64.exe`

---

## 1. Homebrew (macOS + Linux)

**Coverage**: macOS (primary), Linux (secondary)
**Install command**: `brew tap rw3iss/slackers && brew install slackers`
**Effort**: ~30 minutes

### Steps

1. Create a new GitHub repo: `rw3iss/homebrew-slackers`
2. Add a formula file `Formula/slackers.rb`:

```ruby
class Slackers < Formula
  desc "Lightweight terminal-based Slack client"
  homepage "https://github.com/rw3iss/slackers"
  version "0.12.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/rw3iss/slackers/releases/download/v0.12.0/slackers-darwin-arm64"
      sha256 "REPLACE_WITH_SHA256"
    else
      url "https://github.com/rw3iss/slackers/releases/download/v0.12.0/slackers-darwin-amd64"
      sha256 "REPLACE_WITH_SHA256"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/rw3iss/slackers/releases/download/v0.12.0/slackers-linux-arm64"
      sha256 "REPLACE_WITH_SHA256"
    else
      url "https://github.com/rw3iss/slackers/releases/download/v0.12.0/slackers-linux-amd64"
      sha256 "REPLACE_WITH_SHA256"
    end
  end

  def install
    binary = Dir["slackers-*"].first || "slackers"
    bin.install binary => "slackers"
  end

  test do
    assert_match "slackers v", shell_output("#{bin}/slackers version")
  end
end
```

3. Generate SHA256 hashes:
```bash
for f in slackers-*; do echo "$f: $(sha256sum $f | cut -d' ' -f1)"; done
```

4. Push to `rw3iss/homebrew-slackers`
5. Test: `brew tap rw3iss/slackers && brew install slackers`

### Updating for new releases

Update the version and SHA256 hashes in `Formula/slackers.rb` and push.

Consider automating with a GitHub Action that triggers on new releases.

---

## 2. AUR (Arch Linux)

**Coverage**: Arch Linux, Manjaro, EndeavourOS
**Install command**: `yay -S slackers` or `paru -S slackers`
**Effort**: ~30 minutes

### Steps

1. Create an AUR account at https://aur.archlinux.org
2. Create a `PKGBUILD` file:

```bash
# Maintainer: Ryan Weiss <your-email>
pkgname=slackers
pkgver=0.12.0
pkgrel=1
pkgdesc="Lightweight terminal-based Slack client"
arch=('x86_64' 'aarch64')
url="https://github.com/rw3iss/slackers"
license=('MIT')
depends=()

source_x86_64=("https://github.com/rw3iss/slackers/releases/download/v${pkgver}/slackers-linux-amd64")
source_aarch64=("https://github.com/rw3iss/slackers/releases/download/v${pkgver}/slackers-linux-arm64")

sha256sums_x86_64=('REPLACE_WITH_SHA256')
sha256sums_aarch64=('REPLACE_WITH_SHA256')

package() {
    if [[ "$CARCH" == "x86_64" ]]; then
        install -Dm755 "slackers-linux-amd64" "$pkgdir/usr/bin/slackers"
    else
        install -Dm755 "slackers-linux-arm64" "$pkgdir/usr/bin/slackers"
    fi
}
```

3. Create `.SRCINFO` from the PKGBUILD:
```bash
makepkg --printsrcinfo > .SRCINFO
```

4. Push to AUR:
```bash
git clone ssh://aur@aur.archlinux.org/slackers.git
cp PKGBUILD .SRCINFO slackers/
cd slackers && git add . && git commit -m "Initial upload" && git push
```

---

## 3. COPR (Fedora/RHEL/CentOS)

**Coverage**: Fedora, RHEL, CentOS Stream, Rocky Linux, AlmaLinux
**Install command**: `sudo dnf copr enable rw3iss/slackers && sudo dnf install slackers`
**Effort**: ~1 hour

### Steps

1. Create a COPR account at https://copr.fedorainfracloud.org (uses FAS/Fedora account)
2. Create a new project called `slackers`
3. Create an RPM spec file `slackers.spec`:

```spec
Name:           slackers
Version:        0.12.0
Release:        1%{?dist}
Summary:        Lightweight terminal-based Slack client
License:        MIT
URL:            https://github.com/rw3iss/slackers

%ifarch x86_64
Source0:        https://github.com/rw3iss/slackers/releases/download/v%{version}/slackers-linux-amd64
%endif
%ifarch aarch64
Source0:        https://github.com/rw3iss/slackers/releases/download/v%{version}/slackers-linux-arm64
%endif

%description
Slackers is a lightweight terminal-based Slack client built with Go and Bubbletea.
Features include channel search, message search, file uploads/downloads,
customizable shortcuts, mouse support, and more.

%install
mkdir -p %{buildroot}%{_bindir}
%ifarch x86_64
install -m 755 %{SOURCE0} %{buildroot}%{_bindir}/slackers
%endif
%ifarch aarch64
install -m 755 %{SOURCE0} %{buildroot}%{_bindir}/slackers
%endif

%files
%{_bindir}/slackers
```

4. Build the SRPM:
```bash
rpmbuild -bs slackers.spec
```

5. Upload the SRPM to COPR via the web UI or CLI:
```bash
copr-cli build slackers slackers-0.12.0-1.src.rpm
```

---

## 4. Debian/Ubuntu (apt via Packagecloud)

**Coverage**: Debian, Ubuntu, Linux Mint, Pop!_OS
**Install command**: `curl -s https://packagecloud.io/install/repositories/rw3iss/slackers/script.deb.sh | sudo bash && sudo apt install slackers`
**Effort**: ~1-2 hours

### Steps

1. Create a Packagecloud account at https://packagecloud.io (free for open source)
2. Create a repository called `slackers`
3. Build a `.deb` package:

```bash
# Create package structure
mkdir -p slackers_0.12.0/DEBIAN
mkdir -p slackers_0.12.0/usr/bin

# Copy binary
cp slackers-linux-amd64 slackers_0.12.0/usr/bin/slackers
chmod 755 slackers_0.12.0/usr/bin/slackers

# Create control file
cat > slackers_0.12.0/DEBIAN/control << EOF
Package: slackers
Version: 0.12.0
Section: net
Priority: optional
Architecture: amd64
Maintainer: Ryan Weiss <your-email>
Description: Lightweight terminal-based Slack client
 Slackers is a TUI Slack client with channel search, message search,
 file uploads/downloads, customizable shortcuts, and more.
Homepage: https://github.com/rw3iss/slackers
EOF

# Build the .deb
dpkg-deb --build slackers_0.12.0
```

4. Push to Packagecloud:
```bash
package_cloud push rw3iss/slackers/ubuntu/jammy slackers_0.12.0.deb
package_cloud push rw3iss/slackers/debian/bookworm slackers_0.12.0.deb
```

### Alternative: Launchpad PPA

1. Create a Launchpad account
2. Set up a PPA at https://launchpad.net/~rw3iss/+activate-ppa
3. Upload source packages (more complex — requires full Debian packaging with `debian/` directory)
4. Users: `sudo add-apt-repository ppa:rw3iss/slackers && sudo apt install slackers`

---

## 5. Snap Store

**Coverage**: Ubuntu (pre-installed), most Linux distros
**Install command**: `snap install slackers`
**Effort**: ~1 hour

### Steps

1. Create a Snapcraft account at https://snapcraft.io
2. Create `snap/snapcraft.yaml`:

```yaml
name: slackers
version: '0.12.0'
summary: Lightweight terminal-based Slack client
description: |
  Slackers is a TUI Slack client with channel search, message search,
  file uploads/downloads, customizable shortcuts, mouse support, and more.

grade: stable
confinement: classic
base: core22

architectures:
  - build-on: amd64
  - build-on: arm64

parts:
  slackers:
    plugin: go
    source: .
    source-type: git
    build-snaps: [go/1.22/stable]

apps:
  slackers:
    command: bin/slackers
```

3. Build: `snapcraft`
4. Publish: `snapcraft upload slackers_0.12.0_amd64.snap --release=stable`

---

## 6. Go Install (already works)

**Coverage**: Anyone with Go installed
**Install command**: `go install github.com/rw3iss/slackers/cmd/slackers@latest`
**Effort**: 0 (already works)

---

## 7. Nix (nixpkgs)

**Coverage**: NixOS, any system with Nix
**Install command**: `nix-env -iA nixpkgs.slackers`
**Effort**: ~1 hour

### Steps

1. Fork `github.com/NixOS/nixpkgs`
2. Add a derivation at `pkgs/applications/networking/instant-messengers/slackers/default.nix`:

```nix
{ lib, fetchurl, stdenv }:

stdenv.mkDerivation rec {
  pname = "slackers";
  version = "0.12.0";

  src = fetchurl {
    url = "https://github.com/rw3iss/slackers/releases/download/v${version}/slackers-linux-amd64";
    sha256 = "REPLACE_WITH_SHA256";
  };

  dontUnpack = true;

  installPhase = ''
    mkdir -p $out/bin
    cp $src $out/bin/slackers
    chmod +x $out/bin/slackers
  '';

  meta = with lib; {
    description = "Lightweight terminal-based Slack client";
    homepage = "https://github.com/rw3iss/slackers";
    license = licenses.mit;
    platforms = platforms.linux ++ platforms.darwin;
  };
}
```

3. Submit a PR to nixpkgs

---

## Automation: GitHub Actions for releases

To automate package updates on each release, create `.github/workflows/release-packages.yml`:

```yaml
name: Update Packages
on:
  release:
    types: [published]

jobs:
  homebrew:
    runs-on: ubuntu-latest
    steps:
      - name: Update Homebrew formula
        run: |
          # Clone tap, update version and hashes, push
          # (use a bot token with push access to homebrew-slackers)

  aur:
    runs-on: ubuntu-latest
    steps:
      - name: Update AUR package
        run: |
          # Update PKGBUILD version and hashes, push to AUR
          # (use SSH key registered with AUR)

  copr:
    runs-on: ubuntu-latest
    steps:
      - name: Build and push to COPR
        run: |
          # Update spec, build SRPM, push via copr-cli
```

---

## Priority Order

| Priority | Channel | Coverage | Effort | Impact |
|----------|---------|----------|--------|--------|
| 1 | Homebrew tap | macOS + Linux | 30 min | High — standard for CLI tools |
| 2 | AUR | Arch ecosystem | 30 min | High — developer-heavy user base |
| 3 | Go install | Go users | 0 | Medium — already works |
| 4 | COPR | Fedora/RHEL | 1 hour | Medium |
| 5 | Packagecloud/PPA | Debian/Ubuntu | 1-2 hours | Medium |
| 6 | Snap | Ubuntu + others | 1 hour | Medium |
| 7 | Nix | NixOS + Nix users | 1 hour | Low-medium |

## Quick start: do these first

1. **Homebrew tap** — create `rw3iss/homebrew-slackers`, add formula
2. **AUR** — create account, push PKGBUILD
3. **Add `go install` to README** — zero effort, already works
4. **Automate with GitHub Actions** — update packages on each release
