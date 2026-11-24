class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.31"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.31/aviary_v0.7.31_darwin_arm64.tar.gz"
      sha256 "c7a18ff9c8981a5ddb9b838665afebe20d0265474ddfc08037c6bd68c60bff5a"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.31/aviary_v0.7.31_darwin_amd64.tar.gz"
      sha256 "ff206865bed198931114334f4b00f4e8068b4c41a7324c32cae92c532e93c10a"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.31/aviary_v0.7.31_linux_arm64.tar.gz"
      sha256 "ad1da7bd146da14c0bafca06c1ba42293901150ee4808acc6300dc9806541022"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.31/aviary_v0.7.31_linux_amd64.tar.gz"
      sha256 "5c7a1488b742b478e1a59e901ffce97bcd68152f310f6a0312cb9df64e11e1c4"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
