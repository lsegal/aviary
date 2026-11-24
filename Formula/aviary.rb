class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.8"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.8/aviary_v0.7.8_darwin_arm64.tar.gz"
      sha256 "8ca13e968b435080b49db7f91048709d62722532b5117404999d962107140ab2"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.8/aviary_v0.7.8_darwin_amd64.tar.gz"
      sha256 "891d484f5fb32ddbda52345441d3af47d3d65708bfbfe5ce421ae2acf09a80a7"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.8/aviary_v0.7.8_linux_arm64.tar.gz"
      sha256 "bea208774e7d06f63e20fc8ea8720eb216b25e3f3e26987bed7a540184a920d2"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.8/aviary_v0.7.8_linux_amd64.tar.gz"
      sha256 "5e8755e44841e67a0679efcce8b29f4ceb8e579eda2dcab5b0efe1a552f965a7"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
