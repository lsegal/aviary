class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.11"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.11/aviary_v0.7.11_darwin_arm64.tar.gz"
      sha256 "869ed12cfe2ad1fb243b5af4e34703c1c529209f6a93cee9c5b5d2442fdd0127"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.11/aviary_v0.7.11_darwin_amd64.tar.gz"
      sha256 "92e997704a5a9fb039bd04e5782e4ac50ffbf1975eae25744e5144c8820b429a"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.11/aviary_v0.7.11_linux_arm64.tar.gz"
      sha256 "39da262e5521651000d5ce56640b4a1cdf34a3561acaa28cdad50912c38a3fb8"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.11/aviary_v0.7.11_linux_amd64.tar.gz"
      sha256 "0340037b46f7c6c99c026c0ca54eaf4a2bfc184ba973b0996406c417de0d3155"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
