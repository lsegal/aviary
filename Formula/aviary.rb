class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.19"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.19/aviary_v0.7.19_darwin_arm64.tar.gz"
      sha256 "fd2869c46da421d412140baf95fba14b83c68d12c6490a5af5a9c9e9a9b5269f"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.19/aviary_v0.7.19_darwin_amd64.tar.gz"
      sha256 "6d45c90f3ebed0bbaaf650bf508307e2a4d8c5916311064f440f8fab995e2430"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.19/aviary_v0.7.19_linux_arm64.tar.gz"
      sha256 "1af5fc7e4a97cb27d8a494dca8ea261346fca3720e891e66dca4df8a26f0124f"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.19/aviary_v0.7.19_linux_amd64.tar.gz"
      sha256 "3246ff97a9fd2d4b27a97152c799c7d30fa36efa23d244bb0402a9c6b9322982"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
