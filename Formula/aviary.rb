class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.70"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.70/aviary_v0.7.70_darwin_arm64.tar.gz"
      sha256 "5b1628bdcc54e92479cdbb0be2139c2e18140e0ab981cda5a0914865a7ccb48d"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.70/aviary_v0.7.70_darwin_amd64.tar.gz"
      sha256 "2feef01d2b23772f49aeb02a592a95115efef66c01311bbaa43bd7cd6c74fe6a"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.70/aviary_v0.7.70_linux_arm64.tar.gz"
      sha256 "68ce716404dcbc853bc39493d4c5be490039b9e39754e186b14a19e67a4b0742"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.70/aviary_v0.7.70_linux_amd64.tar.gz"
      sha256 "43ead003c1d5a5d262759fc9602dd0e33be7824cb4825d0e6902681ec8494510"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
