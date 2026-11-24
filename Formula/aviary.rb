class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.35"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.35/aviary_v0.7.35_darwin_arm64.tar.gz"
      sha256 "ee9a2401ee726dd5cd6ac9073ee81cadcf6183993387c7c144cb6a1b8c2ab71b"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.35/aviary_v0.7.35_darwin_amd64.tar.gz"
      sha256 "68b8060b01554b8e26d026bbfa15a7a830e9ee16a2da2ce04741413106ea13be"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.35/aviary_v0.7.35_linux_arm64.tar.gz"
      sha256 "2c63b1698288b87c9e8aff98a216f99fc5e6a86ec9375a50b63475a525f892f7"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.35/aviary_v0.7.35_linux_amd64.tar.gz"
      sha256 "5fe680330702866cfc82928f5bd24009ffeb722297fa810a98e94bae1b622857"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
