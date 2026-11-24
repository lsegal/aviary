class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.34"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.34/aviary_v0.7.34_darwin_arm64.tar.gz"
      sha256 "354c3b5722ddc04aeb5ba377076d64b5221e5b9510d3b0c6192c463c314a001c"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.34/aviary_v0.7.34_darwin_amd64.tar.gz"
      sha256 "7ceb6e075b52742a793bd17995473c8ad954e2ec4abe69e9038d15edb6d1c24d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.34/aviary_v0.7.34_linux_arm64.tar.gz"
      sha256 "095198e47e9c259e57bbcb9815f2d0031f22041613228b89117f1c518c15d6ef"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.34/aviary_v0.7.34_linux_amd64.tar.gz"
      sha256 "33746dd034ba4257321b7abef5311770cfec177edfeadf2486c6a7658625bee4"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
