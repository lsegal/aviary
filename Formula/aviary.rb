class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.26"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.26/aviary_v0.7.26_darwin_arm64.tar.gz"
      sha256 "239f0778f6e4b383bacd9ca944f6073d9b952532d37dd2250f1a5c7666bcc125"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.26/aviary_v0.7.26_darwin_amd64.tar.gz"
      sha256 "618234f0daf030b4cd4a1a1170b3f84b238e3778af3236c828ba6b5f79cd8520"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.26/aviary_v0.7.26_linux_arm64.tar.gz"
      sha256 "80af701e5e8c7b84f6e8b5578ed171586f99479066fa34c5192cfa8f4990d2d0"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.26/aviary_v0.7.26_linux_amd64.tar.gz"
      sha256 "5f9b789b6ca56cbc01c972fb7015ccb41e66fa0ef1e49839bf6cbf2f688451b1"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
