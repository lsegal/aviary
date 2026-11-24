class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.28"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.28/aviary_v0.7.28_darwin_arm64.tar.gz"
      sha256 "4f40dc164211f37d4ec477aa743cdb7f4aa2498f728785aab516b358f1de5ce3"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.28/aviary_v0.7.28_darwin_amd64.tar.gz"
      sha256 "0f2f90782576e10ea5c34f10e2258e59e49f4a72a5e588bb926a2af538407e7a"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.28/aviary_v0.7.28_linux_arm64.tar.gz"
      sha256 "7e29a02cbf6cfb1fc432dbd4851bbf4720f3fe6cbf9e26a23a2f143df1395903"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.28/aviary_v0.7.28_linux_amd64.tar.gz"
      sha256 "c07c075139794738b3b2a18149e9f844bdd5c901ad0d1214f1cd9ac22e24afc7"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
