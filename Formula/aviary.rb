class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.37"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.37/aviary_v0.7.37_darwin_arm64.tar.gz"
      sha256 "1e38a897317fa78059fd925f87cfd8c1ecd85e28940c84ddcf5aa353645e9413"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.37/aviary_v0.7.37_darwin_amd64.tar.gz"
      sha256 "859650c651aa4a157a83006073edd196952fea5afa88ca6c6cf7374e95411370"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.37/aviary_v0.7.37_linux_arm64.tar.gz"
      sha256 "5c04bc851ea3ec9f2a74a8a51bd92a377709f27a64c37e8f443d0498bfe34ac9"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.37/aviary_v0.7.37_linux_amd64.tar.gz"
      sha256 "e7675ea07007c7afd0a7c7e3c4d45e0924e6adc2af3e2999996d8b908f1d70b2"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
