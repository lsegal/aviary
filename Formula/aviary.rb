class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.29"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.29/aviary_v0.7.29_darwin_arm64.tar.gz"
      sha256 "2633a1b605e22cd1c199d7ec3a7913dce90393e7f1e38e01fe720c03cdd45cde"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.29/aviary_v0.7.29_darwin_amd64.tar.gz"
      sha256 "5510336e831b1b249a96b937895ec8124156c8347ef4e93043862d58057a8eb5"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.29/aviary_v0.7.29_linux_arm64.tar.gz"
      sha256 "1fe7195d504126b6c88462c5da82ec7c3df545730eb5ee74049f65c0b08c0e1f"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.29/aviary_v0.7.29_linux_amd64.tar.gz"
      sha256 "7b04c74feeaee735d38c9091b017b4e45bb3264009e892892613ed7c4e83e4b0"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
