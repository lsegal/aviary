class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.53"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.53/aviary_v0.7.53_darwin_arm64.tar.gz"
      sha256 "b5dea8cc9850b32e332515e9b9ab322334e2fe676c6c9c85b0f9e497a803069c"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.53/aviary_v0.7.53_darwin_amd64.tar.gz"
      sha256 "da32053d1f9b51753dee34c505acf9539500aadd1cdbe96b28d26d360d0e87a2"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.53/aviary_v0.7.53_linux_arm64.tar.gz"
      sha256 "da7113ea9694dcf6505587667e88773ca69808ab7d83824d31e3ee242631ed5f"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.53/aviary_v0.7.53_linux_amd64.tar.gz"
      sha256 "8bd7309fa970048edca41283e8d1cb4aeb4f78e7f824e4e687c8918e8ef8ed78"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
