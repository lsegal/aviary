class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.39"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.39/aviary_v0.7.39_darwin_arm64.tar.gz"
      sha256 "62152344673b3aa4bbf04382da6d62d97ee4513495bc95cf3b88ce40392711ac"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.39/aviary_v0.7.39_darwin_amd64.tar.gz"
      sha256 "c3daed8e367c1eeea8a1d251b6f57da9669f20583472e2ca2f84233cdcc119f7"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.39/aviary_v0.7.39_linux_arm64.tar.gz"
      sha256 "3f3e526422e15b9e8ac4ad9490e886240feabcca9e10e6ce5659bcb0c01719ea"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.39/aviary_v0.7.39_linux_amd64.tar.gz"
      sha256 "f46aea842c1d40b1816359785202748a56bb6db34066dc4e8133b5d3e7c14a49"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
