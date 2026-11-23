class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.6"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.6/aviary_v0.7.6_darwin_arm64.tar.gz"
      sha256 "321e64ff56a1a4be03c39a73db0384e5eceaf62e0a6c8eb3c129f3976a9ee7b1"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.6/aviary_v0.7.6_darwin_amd64.tar.gz"
      sha256 "13bf06af245b73ba4097839d360ff5a98a8c38144c8a3c848c7d31898a63da95"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.6/aviary_v0.7.6_linux_arm64.tar.gz"
      sha256 "8a6a56aa33bac11fda1ca4ad7b451217f7323755931d2c9fe482a172d7791c1c"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.6/aviary_v0.7.6_linux_amd64.tar.gz"
      sha256 "9951c4c5b09770ecbc37ef7e274bfc93a3d5c3b039857171221f7e7053927687"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
