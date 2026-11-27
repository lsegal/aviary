class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "1.0.1"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v1.0.1/aviary_v1.0.1_darwin_arm64.tar.gz"
      sha256 "ac3c8e8f2a9309cf909c62328a6a9c36f33f7927194e7e6e53c73ce14ed1731d"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v1.0.1/aviary_v1.0.1_darwin_amd64.tar.gz"
      sha256 "e471af8f1d4643afc415bfb0dfc849723cc50b5cb7fb610d256c97b5fa70b000"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v1.0.1/aviary_v1.0.1_linux_arm64.tar.gz"
      sha256 "e60ad8f14505f09e1306129b60feaecd46720687fe9180769dbd86f91271f631"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v1.0.1/aviary_v1.0.1_linux_amd64.tar.gz"
      sha256 "0128aa5f7e5c933d512e13090e097d47a2ea274dba8f980b58031c1b8fe92df5"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
