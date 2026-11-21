class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.0"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.0/aviary_v0.7.0_darwin_arm64.tar.gz"
      sha256 "af3b5969b6100b1b34732faf456385de1f4b306cab445f37355d64a70e3204e8"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.0/aviary_v0.7.0_darwin_amd64.tar.gz"
      sha256 "50c0fa459b23edd7711e5a156f14ecd8e175217900d75349d1f723fc03437296"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.0/aviary_v0.7.0_linux_arm64.tar.gz"
      sha256 "dee88a47221449865f55bb4436d8b50c135f787ca9f59b4a27bb2e2c8ec3eb88"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.0/aviary_v0.7.0_linux_amd64.tar.gz"
      sha256 "70960286ec4908c4773f38e62d31bff08231d5297a15abdfb4f0d26808a0423c"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
