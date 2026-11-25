class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.41"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.41/aviary_v0.7.41_darwin_arm64.tar.gz"
      sha256 "fb476931f0c19ed8bf49b9029a5ec8e158db8c2360faaeabdb45a660ba0cf17e"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.41/aviary_v0.7.41_darwin_amd64.tar.gz"
      sha256 "c32e373b26096e9d9b56b91b7d0d8f27856ab60e3970c5a962dcba8e7ef36dad"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.41/aviary_v0.7.41_linux_arm64.tar.gz"
      sha256 "b0227a05622ac81f9777081d408b7436b1820ea1e017c3890931bd6efb167036"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.41/aviary_v0.7.41_linux_amd64.tar.gz"
      sha256 "f4423f2055b7285b82a3fc199db4d420ab5a2ccd542b16d4c7dd1294f1c2e798"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
