class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.42"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.42/aviary_v0.7.42_darwin_arm64.tar.gz"
      sha256 "568674bb0e8af3d5ef92f569f4761f55e0bd5e530675f3bf47f5cfca15a5215b"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.42/aviary_v0.7.42_darwin_amd64.tar.gz"
      sha256 "edfb9fbfc16daeefe756c5b3ca62e5feee969b52c8b31751d0182c7a123806bc"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.42/aviary_v0.7.42_linux_arm64.tar.gz"
      sha256 "1ec58855df376d967256bacb04bbb0d9645646fd85cbac03e80660777b784e01"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.42/aviary_v0.7.42_linux_amd64.tar.gz"
      sha256 "a1edb5b0a7bec4681a50c311f6cca1e18be902ea60be22ce9748da27c039e2c3"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
