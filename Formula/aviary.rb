class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.68"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.68/aviary_v0.7.68_darwin_arm64.tar.gz"
      sha256 "b255a6141f84709ae702387fad2eb05a0954a2c150670527dd863490097b5271"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.68/aviary_v0.7.68_darwin_amd64.tar.gz"
      sha256 "357040e0b049963b20e1904b197ddc14fd37bda1e6aff6bb581f751177784a4b"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.68/aviary_v0.7.68_linux_arm64.tar.gz"
      sha256 "a93cee08bf956ab4fe910429af3a48817348aae492542f4970596d21d370ae52"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.68/aviary_v0.7.68_linux_amd64.tar.gz"
      sha256 "b30c6f22d434b6c7d47a1d1e841c00a38bde9eafffcb2520c75ef964c5078718"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
