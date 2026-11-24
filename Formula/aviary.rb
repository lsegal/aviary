class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.32"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.32/aviary_v0.7.32_darwin_arm64.tar.gz"
      sha256 "7074b72cd9afe5886ed7bcba590d9fc5a352b8dea58a7701b434e768dbfd5cb5"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.32/aviary_v0.7.32_darwin_amd64.tar.gz"
      sha256 "e1f819060f70500e554c2d6567a3a3ba4cdd917e17b2fb06627fcd0acd5c8a4f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.32/aviary_v0.7.32_linux_arm64.tar.gz"
      sha256 "59a22ab2368f1477f946f2b01bae17153bdc90a8732d3c1478c9da98eacb0e51"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.32/aviary_v0.7.32_linux_amd64.tar.gz"
      sha256 "c4a1eb11991dd3b8ed74983d6bde11bf054a154f1705cf286bb6008e2f053937"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
