class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.7"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.7/aviary_v0.7.7_darwin_arm64.tar.gz"
      sha256 "e9f028e966ebf186b278c9e958232dca066785bc757b4ae24054c5b47b61c9a5"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.7/aviary_v0.7.7_darwin_amd64.tar.gz"
      sha256 "aa96ad14e3b1f3fcec9c2fe830a7af2922eb8221e344efd4895aac5e019bd082"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.7/aviary_v0.7.7_linux_arm64.tar.gz"
      sha256 "fba267f62d3a5a6f0248147c68ab8c91f789fcd0a5450977b252110e6a493d20"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.7/aviary_v0.7.7_linux_amd64.tar.gz"
      sha256 "869f0bd968650872fe4eadb8597a5e35b3957d1f3ca437b393a5e1ad7aa61677"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
