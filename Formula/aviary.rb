class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.4.9"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.9/aviary_v0.4.9_darwin_arm64.tar.gz"
      sha256 "600c8c838401bd8e40948b0659b8bbf09e247dfeb517971e5190e490ba0392da"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.9/aviary_v0.4.9_darwin_amd64.tar.gz"
      sha256 "b0b5a55e3de0f9d7a57fd7d1eedc77fccdf0fad7dc0490fdb7f4b96ff73cf68f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.9/aviary_v0.4.9_linux_arm64.tar.gz"
      sha256 "acb0383d60b7b0a24c6436dc6ed5ee264fe2cd13815deba489cab0e84af76674"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.9/aviary_v0.4.9_linux_amd64.tar.gz"
      sha256 "fb4f606693ba857a2bf682b045079386ad1f43b4c694552ceaac76844c4fa1b4"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
