class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.62"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.62/aviary_v0.7.62_darwin_arm64.tar.gz"
      sha256 "75a790027af896c858d858f578c5b5e93aaf33620116f1243b2ab2735baa4af7"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.62/aviary_v0.7.62_darwin_amd64.tar.gz"
      sha256 "a0b57352c506dcbf0a538af4547199dcd61c46d2b2e683d660d725eff1029de2"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.62/aviary_v0.7.62_linux_arm64.tar.gz"
      sha256 "48a9005d737cb89c2e0da4e82bad18bc47384d16bc7a5774373398f2cad40092"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.62/aviary_v0.7.62_linux_amd64.tar.gz"
      sha256 "302ed6284586226a9f1bba7ec46b994f1f21cc8c58f0e83cf6d6c7c443c771af"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
