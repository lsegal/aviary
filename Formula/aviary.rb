class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.56"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.56/aviary_v0.7.56_darwin_arm64.tar.gz"
      sha256 "e972ec93ea56627b6ff614b9c3c5af87c2b5993f0c496d67d292c2ce6cba19aa"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.56/aviary_v0.7.56_darwin_amd64.tar.gz"
      sha256 "4cb310a36eaada87cb73191cbe4ee65a3d0ba060a7335fb0c2364f47db7b9a0a"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.56/aviary_v0.7.56_linux_arm64.tar.gz"
      sha256 "53b13f1561d436b3aa2e4e4a36342a814d7cce026a4839a0758e8e5bf41c3938"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.56/aviary_v0.7.56_linux_amd64.tar.gz"
      sha256 "31b58015f51d9ac0f8ec753aa8cd513b385430750e08daf159e5439543dc7efd"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
