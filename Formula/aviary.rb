class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.74"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.74/aviary_v0.7.74_darwin_arm64.tar.gz"
      sha256 "006d91b358b80046b2f0cce0fdd8b4de3cbdbc32a1e02d301b98227814261865"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.74/aviary_v0.7.74_darwin_amd64.tar.gz"
      sha256 "0ee62fc36e8289a944f9668eadb185e1d3442104222729e1423e533b0e5f0c47"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.74/aviary_v0.7.74_linux_arm64.tar.gz"
      sha256 "b5a33cd8a5968ea16b2d9a7a70c6e43e30f4e0b1e7c2d6c62f4f9bffa4e42bfe"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.74/aviary_v0.7.74_linux_amd64.tar.gz"
      sha256 "6d64b4608aef2075615417c6d5f2e26e1b92cf8d40e92d81482e9267b1f9e037"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
