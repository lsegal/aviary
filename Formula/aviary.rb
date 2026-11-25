class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.58"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.58/aviary_v0.7.58_darwin_arm64.tar.gz"
      sha256 "27b46df64d589577fb082217df226bb47efd980669136dc6f714dae6d267dfeb"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.58/aviary_v0.7.58_darwin_amd64.tar.gz"
      sha256 "eb650804e719d0f100ddbd0e30f512545be8c24a9c089405c3bfc95cdc62d324"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.58/aviary_v0.7.58_linux_arm64.tar.gz"
      sha256 "4c5addd900ba1f0575a23ac8443f8109c9f2e3b0f37a495f62d69cda191c8b3d"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.58/aviary_v0.7.58_linux_amd64.tar.gz"
      sha256 "0323e65e61c3b60bbf02a91034ebb74a68657da0df4721fc3bc068eea355c54a"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
