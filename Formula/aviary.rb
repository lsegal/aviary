class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.44"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.44/aviary_v0.7.44_darwin_arm64.tar.gz"
      sha256 "ab7bb530b5a6e52a13864de96f18a53ae6c10d7974710fd18d1b37c08fe30a50"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.44/aviary_v0.7.44_darwin_amd64.tar.gz"
      sha256 "04b9c426c3ec60cc2c0c3b68ffebed56a7743e0ecf5a838e39aecee73e84e994"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.44/aviary_v0.7.44_linux_arm64.tar.gz"
      sha256 "212ff2c8715555ef032a7b8f74cdb0a4023001071f6987db7a12062fcb66c294"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.44/aviary_v0.7.44_linux_amd64.tar.gz"
      sha256 "3fb3fbe03b822abcffe7ab51cd8c6a86c137c7f6ad830117b6873cdd3cc2d295"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
