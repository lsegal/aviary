class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.75"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.75/aviary_v0.7.75_darwin_arm64.tar.gz"
      sha256 "fd563bd45f2b8a6129fd9a5d09c281df6e77e4ec4d7cc10ef9aa742190f33a1a"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.75/aviary_v0.7.75_darwin_amd64.tar.gz"
      sha256 "7d6e2dfaa9ce3c14bc6b5ddd1011b295960210a0e6d869700405259532aa3557"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.75/aviary_v0.7.75_linux_arm64.tar.gz"
      sha256 "467ce643aeed4c39aad5a72721d09283b6a1e62f742406e3cec2578ad49c95a1"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.75/aviary_v0.7.75_linux_amd64.tar.gz"
      sha256 "4f934571d06b379a06e4f5f40df7498cf6e5814c783c792dbb2777d472d98f8e"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
