class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.69"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.69/aviary_v0.7.69_darwin_arm64.tar.gz"
      sha256 "ee254d902525383b4e7939ef05ebb289811c247c5334978945fe523cebc725f2"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.69/aviary_v0.7.69_darwin_amd64.tar.gz"
      sha256 "dd4bb784bfbb20b7b2798c4668502ba78e56688338b40a42106a2e55c2e84828"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.69/aviary_v0.7.69_linux_arm64.tar.gz"
      sha256 "61d3aefdc98153a4aba9dd57415d55adb9de8fd050ebaedf8cd551035e9a6a1f"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.69/aviary_v0.7.69_linux_amd64.tar.gz"
      sha256 "076e53cd088dbcf8413c963e82b4fcc59f5e227566a1b8ba07078bae13e46e31"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
