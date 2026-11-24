class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.9"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.9/aviary_v0.7.9_darwin_arm64.tar.gz"
      sha256 "552792968269f18f1b732bba8cf74722fe6ceff3b9ff3f72cd3bf56f5ab795aa"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.9/aviary_v0.7.9_darwin_amd64.tar.gz"
      sha256 "1820baa81bdd3f9929eb44d0bfed5476568eba0928f7c0eaa09da6434c68e038"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.9/aviary_v0.7.9_linux_arm64.tar.gz"
      sha256 "8651f8625d5b4956e378c8ffee9528a45d8404433b4ba00c86da64c5b9d3c925"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.9/aviary_v0.7.9_linux_amd64.tar.gz"
      sha256 "50d0fa0091797b985e10076a23c568bd83aa0d08d688fffc772e4bb2a92ac50a"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
