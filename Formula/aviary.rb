class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.47"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.47/aviary_v0.7.47_darwin_arm64.tar.gz"
      sha256 "413379890aa939a08c066ce6b3cc2ab68261c5fd1699b42bd8ee1e865e6c94f6"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.47/aviary_v0.7.47_darwin_amd64.tar.gz"
      sha256 "a78fb8289dca2bcb2765ba03eca8befb7c81e142507fc9ddb27263648d850c05"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.47/aviary_v0.7.47_linux_arm64.tar.gz"
      sha256 "52521195bc80cc31f7784b0798789268069b97398dc6f8fc9e42b7fae5663e03"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.47/aviary_v0.7.47_linux_amd64.tar.gz"
      sha256 "54778b160a10cefbf48803cb4837913d2348d32b7e3b626097eb2d9af32b5d49"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
