class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.85"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.85/aviary_v0.7.85_darwin_arm64.tar.gz"
      sha256 "3023029ab583275a0fd5cbcb5e66ec70da374f86859067bae14582a0b2abae72"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.85/aviary_v0.7.85_darwin_amd64.tar.gz"
      sha256 "2dae5e351b74b9e89c78c1c5a042a04c734f1e5141b7e4893054256094a8a81d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.85/aviary_v0.7.85_linux_arm64.tar.gz"
      sha256 "93ac42eb37c9660fdcd7958b5e3ce91c80e4e5c8804493d3fdc1ef025ba5f138"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.85/aviary_v0.7.85_linux_amd64.tar.gz"
      sha256 "9980d75822320604f80dea253e866953f4be50c2a9bf11fd94ed2c2b22b569b7"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
