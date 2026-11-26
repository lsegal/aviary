class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.61"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.61/aviary_v0.7.61_darwin_arm64.tar.gz"
      sha256 "e67f2919d34e02f72d7a1b52a73becc95096093b2d8d25ddaee51e463c59d429"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.61/aviary_v0.7.61_darwin_amd64.tar.gz"
      sha256 "13cd27e85895c190d041e2181d7baaaba0fca37cb00ca6782cd2068969ee911b"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.61/aviary_v0.7.61_linux_arm64.tar.gz"
      sha256 "7c076c1f243bd31aec56991bda903b5dd1450334ea2111ef7c4415937fc7ea6c"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.61/aviary_v0.7.61_linux_amd64.tar.gz"
      sha256 "e4f5cd78743daef7d34f7af3ced92678defc3b49089966cce4e9790aff6453f8"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
