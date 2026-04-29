class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.4.8"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.8/aviary_v0.4.8_darwin_arm64.tar.gz"
      sha256 "03e23f39155046d0b1264379c94bc3835abe8a8d9f5c52d31fef8556a652d7a0"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.8/aviary_v0.4.8_darwin_amd64.tar.gz"
      sha256 "0f1f1dbfe83d735c0c8e7b59961a4e259fb52829486ea31f17f1209a787726c0"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.8/aviary_v0.4.8_linux_arm64.tar.gz"
      sha256 "226edb9f5e86d00c98ee5ecf7606548d6b91870fac91c17a422b702020647f5f"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.8/aviary_v0.4.8_linux_amd64.tar.gz"
      sha256 "4263ca1c412a0575e9fc3ea6a0b4f96d615c2c9438fc9bfee73ae10f954e6140"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
