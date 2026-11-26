class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.63"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.63/aviary_v0.7.63_darwin_arm64.tar.gz"
      sha256 "13445cd759d64a3eb1b01fa641a3cf5d5f96b09bd4b7f581075ae79a0e766797"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.63/aviary_v0.7.63_darwin_amd64.tar.gz"
      sha256 "e45fd3db08e5b662b9a5f4cbbc7fdd3eaae760554f31273d1479dccbb5870ec5"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.63/aviary_v0.7.63_linux_arm64.tar.gz"
      sha256 "ac62c36538b1d03657fa64e623390a02208e4dd08554277cae8153febddd6517"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.63/aviary_v0.7.63_linux_amd64.tar.gz"
      sha256 "a500bdf3e60765b333c420dcf066dc34a91ca780fc3505d0a2d1de0e237e27ee"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
