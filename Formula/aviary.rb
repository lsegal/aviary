class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.14"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.14/aviary_v0.7.14_darwin_arm64.tar.gz"
      sha256 "bb4b804cb208a959a55098450f650a234708df0aa03a195563f45850bb52a9bc"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.14/aviary_v0.7.14_darwin_amd64.tar.gz"
      sha256 "26348410cfb0ee6a21477117dc996d056c0b966448781802e9ad80a95d4c5aef"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.14/aviary_v0.7.14_linux_arm64.tar.gz"
      sha256 "659d8a89785f5d8eeadefd317c6e3420478d02b91ce1b683e447974f2de54a97"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.14/aviary_v0.7.14_linux_amd64.tar.gz"
      sha256 "4d548c02a9f19f9e7a503014f5a44746ed71bc5a75627bf93c3cf78a8a2937b0"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
