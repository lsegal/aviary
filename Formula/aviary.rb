class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.25"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.25/aviary_v0.7.25_darwin_arm64.tar.gz"
      sha256 "da70452a9e336d5bad8a572f6de7aa7306e23f1807843a687accfdb4ca0cb13b"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.25/aviary_v0.7.25_darwin_amd64.tar.gz"
      sha256 "de838c0163a883fa6aea05c1458b6cda1ba4e1a4761a0cbc1c4bd535eac603de"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.25/aviary_v0.7.25_linux_arm64.tar.gz"
      sha256 "ac63be9191d5840818b7ee230a90fc4ba2f40818e115c40cf7291b038c29ad21"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.25/aviary_v0.7.25_linux_amd64.tar.gz"
      sha256 "4477dfd160fd14f76683491918ae77ba97e4fa2dfec1200e92775564d12f5cfe"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
