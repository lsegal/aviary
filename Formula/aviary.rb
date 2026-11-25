class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.51"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.51/aviary_v0.7.51_darwin_arm64.tar.gz"
      sha256 "162e3dc439b4032f001d2d6fa11c33cd83fc8ad77bc9b93e259c3ec0158ad5cb"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.51/aviary_v0.7.51_darwin_amd64.tar.gz"
      sha256 "5c16b77c3ecd20c9a716bb66681ac50ff0b54143e9de85d0b6e8c96a036a7162"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.51/aviary_v0.7.51_linux_arm64.tar.gz"
      sha256 "60c51741ef700ac99079d4dc40e5d8dcfebce5b2b850971071999d489eae9870"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.51/aviary_v0.7.51_linux_amd64.tar.gz"
      sha256 "ee539f6307365403c014706bdc744229c5d3fa125dc2ade171570666136474a9"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
