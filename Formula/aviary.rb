class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.6.1"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.6.1/aviary_v0.6.1_darwin_arm64.tar.gz"
      sha256 "d3dfec26e9cd445f62f51d9601dd51ce9e8c6ae0b2974b0448151266da3138a8"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.6.1/aviary_v0.6.1_darwin_amd64.tar.gz"
      sha256 "72d7db3d2317df42103fc979c621ad57fad57a7e39e3e30e278e4c0314eeb917"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.6.1/aviary_v0.6.1_linux_arm64.tar.gz"
      sha256 "2ce07f48b68ccc43bd04f4c713837a3d08473ded8eeb3e21a7f09a7f8e36040e"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.6.1/aviary_v0.6.1_linux_amd64.tar.gz"
      sha256 "84b5b427acc2557bf421f2ba462b90800a058e3366f4579b3cf8152d6ad55a64"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
