class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.4"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.4/aviary_v0.7.4_darwin_arm64.tar.gz"
      sha256 "8b4dc3365674be06237f2acc4198a2f22d62833af753f0f7ce2004e6d15de0d5"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.4/aviary_v0.7.4_darwin_amd64.tar.gz"
      sha256 "3e2e90e8b8e4451c9e97bd81294a156648f573a15f0d87734ad2cc94462ff2ac"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.4/aviary_v0.7.4_linux_arm64.tar.gz"
      sha256 "14cc760f4e5201a0f77bf9822dfed25cea8ae18fe1f2c0eef2ba73b7df2ce486"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.4/aviary_v0.7.4_linux_amd64.tar.gz"
      sha256 "f5a912a9d1c876672ad3522f00377df49929e2d233202551dfe73b5d880f6e32"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
