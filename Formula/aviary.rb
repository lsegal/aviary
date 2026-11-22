class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.2"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.2/aviary_v0.7.2_darwin_arm64.tar.gz"
      sha256 "f1f1b4eb57e5c336957f0273e23e080dbb0cbb172e15d18f41b3a38c5a1f41df"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.2/aviary_v0.7.2_darwin_amd64.tar.gz"
      sha256 "4ddf45d3e1b631afa7928ebdc07f02199165eb0a480c508c8f8c360e64d0b664"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.2/aviary_v0.7.2_linux_arm64.tar.gz"
      sha256 "557df6f489ae2f5f6869a66a5a8c0d9499b5aad5c5ce73753ff4fc9960761eb1"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.2/aviary_v0.7.2_linux_amd64.tar.gz"
      sha256 "695cc8ffeca6d45d78b130fd96c1db705ce6d0d3484b686f88144636131ed2cf"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
