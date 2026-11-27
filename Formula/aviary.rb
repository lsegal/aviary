class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.79"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.79/aviary_v0.7.79_darwin_arm64.tar.gz"
      sha256 "1d2655d3744669b552d562f0bbe59189cca7f7045271e187cf094cb9dbb24690"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.79/aviary_v0.7.79_darwin_amd64.tar.gz"
      sha256 "933973bb0ceedcbd29c89caddfcd73ea0465cd55ddcf5b5085f7eb3aaeb33f72"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.79/aviary_v0.7.79_linux_arm64.tar.gz"
      sha256 "eff170f36bb1bf8494534cc5f7e9b439f207caf4d659e3ddfb8a12eb631045e5"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.79/aviary_v0.7.79_linux_amd64.tar.gz"
      sha256 "95b7f2280c27257517b2c75ef6538cf88527b81d1e2ccc6f40c4c0f27897e45e"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
