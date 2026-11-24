class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.21"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.21/aviary_v0.7.21_darwin_arm64.tar.gz"
      sha256 "c60b386f7bc4610622350e3debc152cf7907c12b60c48eb1dfa7dedcda74c752"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.21/aviary_v0.7.21_darwin_amd64.tar.gz"
      sha256 "cc0cdaed32ea4ef170369cb9daab67360aa151b9a5eee3d3286c56490a0d232c"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.21/aviary_v0.7.21_linux_arm64.tar.gz"
      sha256 "fc5893f955c72d83fa9ac0dbce95d72940a7fe1e0b14cbd995400b6d560af565"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.21/aviary_v0.7.21_linux_amd64.tar.gz"
      sha256 "eed9df7d8df57f846ccecfb314656aecf1d572fe51e6345ba0c6ad703d7f1b39"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
