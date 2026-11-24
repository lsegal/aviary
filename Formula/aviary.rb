class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.10"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.10/aviary_v0.7.10_darwin_arm64.tar.gz"
      sha256 "d444c4dfbb37d9405e246e1e9179a71d6769f86844f6794f515eac6644cbe36f"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.10/aviary_v0.7.10_darwin_amd64.tar.gz"
      sha256 "4dec82db59af65d0dbb7c502dd4009c649552e2503ad39e08cbfd3224d149fd8"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.10/aviary_v0.7.10_linux_arm64.tar.gz"
      sha256 "4b4c58e9ce2e26a583dd6b76ee2a03f2570966b78d800b76188054f001c23a05"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.10/aviary_v0.7.10_linux_amd64.tar.gz"
      sha256 "19db9c7cc6f5fad29a2eb9e0ac2a7d787864086ed9cfc7a4b6fa156674cb556d"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
