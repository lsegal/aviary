class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.65"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.65/aviary_v0.7.65_darwin_arm64.tar.gz"
      sha256 "d4bd5d2950bec85e104d1e989a7de61064aad634ad4958fc614f633510c7d5dd"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.65/aviary_v0.7.65_darwin_amd64.tar.gz"
      sha256 "92bb21b577a39823770154fc12013dc3a864b1cb1f12b86b71403d2414703357"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.65/aviary_v0.7.65_linux_arm64.tar.gz"
      sha256 "5d4c41cfc9da72c85bde668546813c00ee11727ab1a866ad4a90ab92ba6dd315"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.65/aviary_v0.7.65_linux_amd64.tar.gz"
      sha256 "144d50f9239892ab0a02ebbe56df69aa8d007623ec45d5372709c9b3dc4700f5"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
