class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.36"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.36/aviary_v0.7.36_darwin_arm64.tar.gz"
      sha256 "03ce3d30f528a5a86a9ef933d94db83fdeb0e504a6a35a6e212bfe5f6e2660ea"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.36/aviary_v0.7.36_darwin_amd64.tar.gz"
      sha256 "0b99903bfee2c292296c90f030894abe69fdd6fd5df8d8d64cc8e79f4e4f41e7"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.36/aviary_v0.7.36_linux_arm64.tar.gz"
      sha256 "3d79cc9c7326859c8594c44655db5ce70c9b11b03896f671074966643021a2ca"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.36/aviary_v0.7.36_linux_amd64.tar.gz"
      sha256 "675983b50dd389280656edebdf476674a899849a3f3d2f902b10215a537e304e"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
