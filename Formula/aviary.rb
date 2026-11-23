class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.3"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.3/aviary_v0.7.3_darwin_arm64.tar.gz"
      sha256 "b70062767b70e3ef3ca8bf6edd08554f30d2fc442f5a1a98601f98bbb7a0f557"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.3/aviary_v0.7.3_darwin_amd64.tar.gz"
      sha256 "40abb6c015816cc8db23c9a0de823beef76f1cd2f7638eee96023e03d16a4800"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.3/aviary_v0.7.3_linux_arm64.tar.gz"
      sha256 "0b0e25a86ba6fef4439c1e00db131fc62e36aea150f9efb3dc44f61625f2a488"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.3/aviary_v0.7.3_linux_amd64.tar.gz"
      sha256 "89f7186a9fdba6775b2beb12c248ac7d44e0b1a2a9130eda11883c5ef926943b"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
