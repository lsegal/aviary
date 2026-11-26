class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.59"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.59/aviary_v0.7.59_darwin_arm64.tar.gz"
      sha256 "1da955e647ae123ad758af12dae2d029ace98d4762fb6e72e01773189c7b81be"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.59/aviary_v0.7.59_darwin_amd64.tar.gz"
      sha256 "97ae12e980954a0831c60d1175ffde84b7330117d37b038013b6296c1322612f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.59/aviary_v0.7.59_linux_arm64.tar.gz"
      sha256 "78eda0e9955e225668a4017a655b7570cafa08874018827ce9e0d6bc8604aeb0"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.59/aviary_v0.7.59_linux_amd64.tar.gz"
      sha256 "5c20af979f3ede674d71179b1357f195d6baba46551ea938a9474775ab45f56c"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
