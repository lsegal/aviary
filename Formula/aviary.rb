class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.66"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.66/aviary_v0.7.66_darwin_arm64.tar.gz"
      sha256 "e157f697b6f9d3637a8832d0ba442440b54084522e8bc971d64aacaf38deffac"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.66/aviary_v0.7.66_darwin_amd64.tar.gz"
      sha256 "3826f6fa4acdfdf06c5312788d3f4ae32bff5e656fc5a6c8ad505c8bc33e6bfc"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.66/aviary_v0.7.66_linux_arm64.tar.gz"
      sha256 "519cc091e0e7590d7fb8beda3f292305c268ec6ecf1861ce37e15e695997d1cc"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.66/aviary_v0.7.66_linux_amd64.tar.gz"
      sha256 "67170e81ef5e4ad76d11c960671c32e183a60c058cdedf10a20425cd788c70be"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
