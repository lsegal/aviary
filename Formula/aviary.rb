class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.48"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.48/aviary_v0.7.48_darwin_arm64.tar.gz"
      sha256 "73581380012aa4f25c610f7016ad1199c205f878348ebc2b16f50f23668e8642"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.48/aviary_v0.7.48_darwin_amd64.tar.gz"
      sha256 "17b463f9e9d57ca7736163f4457591741eee54423ab1593fa8f420db0f26622b"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.48/aviary_v0.7.48_linux_arm64.tar.gz"
      sha256 "e5ec766da3f0297d3f7524b71229c2219655f001b1eaa1a2987e0d0b53d061f2"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.48/aviary_v0.7.48_linux_amd64.tar.gz"
      sha256 "3f51b605232fc56ea3351d3a2e5e60f92b53ad0e9c273b8957ea7f21551ee1f9"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
