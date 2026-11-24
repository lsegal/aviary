class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.12"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.12/aviary_v0.7.12_darwin_arm64.tar.gz"
      sha256 "49c49ad5d315502fb21810debb46a743fdb96815851faac758bef1a7606e8be7"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.12/aviary_v0.7.12_darwin_amd64.tar.gz"
      sha256 "f4aa5016be03ab3747cf8348eeb11e0649a55ec884827e97c38d83c637a7afef"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.12/aviary_v0.7.12_linux_arm64.tar.gz"
      sha256 "1df16099ff5d523958f346146b9cb777a6caaab8e36b808a84f2a3bf1a3147d0"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.12/aviary_v0.7.12_linux_amd64.tar.gz"
      sha256 "723dbb35bd1f3a90acdebf96354ef9bc82a0be5d1184de9722df32b54d602bb5"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
