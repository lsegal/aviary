class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.5.0"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.5.0/aviary_v0.5.0_darwin_arm64.tar.gz"
      sha256 "1767600f098d229e8e40f6da6093972377c6dd102d44583fac266578726db1fd"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.5.0/aviary_v0.5.0_darwin_amd64.tar.gz"
      sha256 "361c5d2b3ec7333f9a5aa07a0b89aacc993b5cd7660e3afa5ab8f2598e1bf7d2"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.5.0/aviary_v0.5.0_linux_arm64.tar.gz"
      sha256 "1201d5d1743de28cc6428e10bfedc33666326023d64bdd1131945fbd4ba281f8"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.5.0/aviary_v0.5.0_linux_amd64.tar.gz"
      sha256 "0aff49c213d520a42ac6eb6cab0990e48cbfe50c0e8180227b1e1ebeedff5716"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
