class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.64"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.64/aviary_v0.7.64_darwin_arm64.tar.gz"
      sha256 "7d9b353f8e94daba9da5f3c26b025c7929acd1a6015f4a83a555b82ae8be3d0d"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.64/aviary_v0.7.64_darwin_amd64.tar.gz"
      sha256 "62a0f04de4bb8caaa9d79deb8b112edb21436003b9d06017e2c322e24ea6aab2"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.64/aviary_v0.7.64_linux_arm64.tar.gz"
      sha256 "e7dcdfdec6022733c2c7107681987f9571c4ee31b3c1eab99021b1afbf23c838"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.64/aviary_v0.7.64_linux_amd64.tar.gz"
      sha256 "5665084e23ba3bc1a2d860e7b36620e4474771f3f808f50b1d743acfc4e5a7c0"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
