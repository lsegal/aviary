class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.81"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.81/aviary_v0.7.81_darwin_arm64.tar.gz"
      sha256 "3dfc6f72d67986e8c5cdf1416a8226aeb65062e755b87b0fcd6b67780eeb047d"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.81/aviary_v0.7.81_darwin_amd64.tar.gz"
      sha256 "a0aeb51a8a05b89967dc469830b3aa0df5cbd5a9526be1dea96e705bf04ab2f2"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.81/aviary_v0.7.81_linux_arm64.tar.gz"
      sha256 "140b7955a2065dad8567dcb6b6ca9bde64969312d76387b69a9890d917870e26"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.81/aviary_v0.7.81_linux_amd64.tar.gz"
      sha256 "57b557d0f25f4098a265596e99d725f76760de7fd6d0184ea25304b78c006118"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
