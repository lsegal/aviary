class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.77"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.77/aviary_v0.7.77_darwin_arm64.tar.gz"
      sha256 "f5991c320886ba8987fe67aa50b08002a8abb4bab87fbbf135beb6bc7695bc45"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.77/aviary_v0.7.77_darwin_amd64.tar.gz"
      sha256 "1ed5913a4c2bac428d1412f1c8e520ec4c814910c69bedd38b894bec2acf4d40"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.77/aviary_v0.7.77_linux_arm64.tar.gz"
      sha256 "ab5f1f4d569735be1eb1015a08c8f0d61fa73246842821b27f88ed5ed2d53330"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.77/aviary_v0.7.77_linux_amd64.tar.gz"
      sha256 "d470ab9799947223b00051ad9afb1f409e5392311368fca18fae94504c62d004"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
