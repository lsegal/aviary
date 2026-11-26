class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.76"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.76/aviary_v0.7.76_darwin_arm64.tar.gz"
      sha256 "a1aab466c8ff85164f5aff629a3dd0bfb968c84fa6568c2edcc85f2f93223f33"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.76/aviary_v0.7.76_darwin_amd64.tar.gz"
      sha256 "c3e4632f0907e9e6599d320aa67f7caf1fe1d256832eef274c70769ea81abc59"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.76/aviary_v0.7.76_linux_arm64.tar.gz"
      sha256 "20bb46857d09d13e5b1f0b62a485abf31bfabcdb5b080ca334c0dbe309ee171c"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.76/aviary_v0.7.76_linux_amd64.tar.gz"
      sha256 "133a4645f098cb12492ee6cfa36e2e0eed5bdda4af2ca6091000af2a6b0dff08"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
