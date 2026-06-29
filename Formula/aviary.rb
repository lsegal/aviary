class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.6.0"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.6.0/aviary_v0.6.0_darwin_arm64.tar.gz"
      sha256 "d4361c8a2a9e4b9064c7011f873d3a808ce83dde3bff6022f73e6a65acb03cb5"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.6.0/aviary_v0.6.0_darwin_amd64.tar.gz"
      sha256 "2f033ad58548f97527b611bed8701bbc4a1bb00cb71b58411250eb9c6bd5c456"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.6.0/aviary_v0.6.0_linux_arm64.tar.gz"
      sha256 "bb981106250a93c02d4316edf81b4aced65b5ac392399be1d152a57446186fd0"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.6.0/aviary_v0.6.0_linux_amd64.tar.gz"
      sha256 "ac1daa1e97bbf0ef8fcd2485c98b6b62e512a9596fe732892e21abef5be8346b"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
