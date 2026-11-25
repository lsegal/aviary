class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.57"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.57/aviary_v0.7.57_darwin_arm64.tar.gz"
      sha256 "ce90269e640af8d2084af771b3038745f10a2d8d74d266bf9c4c6f794bd6ed42"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.57/aviary_v0.7.57_darwin_amd64.tar.gz"
      sha256 "8d302bc2d9969808d7ce445d759855121f09d9ed78b97cd1bba393ba864a18d7"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.57/aviary_v0.7.57_linux_arm64.tar.gz"
      sha256 "98eba0f44e6a09f7f98a16b909974aad855a324accec22c7cdea649d3e4fd1fa"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.57/aviary_v0.7.57_linux_amd64.tar.gz"
      sha256 "66e4fcf814a4fc1e8d5bd31fa7a60d8df160585643a9050cff7ff0514abdf80f"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
