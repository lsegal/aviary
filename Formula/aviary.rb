class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.60"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.60/aviary_v0.7.60_darwin_arm64.tar.gz"
      sha256 "cf7182c5b36a5ca6fc2b74758c6e6db954f312c94b2b7342ae43bf1508141398"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.60/aviary_v0.7.60_darwin_amd64.tar.gz"
      sha256 "a32927958b8d317342004a3f4a40241c8a09069294ab47e1df8deb6d3bf7b5a1"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.60/aviary_v0.7.60_linux_arm64.tar.gz"
      sha256 "f8dfe8fc58684bfc7a8039b42bd2d80a055bcb05a3c2da59b27055a4ccd5f87a"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.60/aviary_v0.7.60_linux_amd64.tar.gz"
      sha256 "cbd2a4bc781272619df4668a6e4ee3027423707df1f9dbcd71475aea593b46de"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
