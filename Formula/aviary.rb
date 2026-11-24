class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.27"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.27/aviary_v0.7.27_darwin_arm64.tar.gz"
      sha256 "27063c5fda81d43998d76d5f5911d3393c9455a73c9621330988ed5fb8d5c8ef"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.27/aviary_v0.7.27_darwin_amd64.tar.gz"
      sha256 "c091087c7295e7b08763f9e73e65fb7333de0effb79699da0cf6e34dffa555c8"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.27/aviary_v0.7.27_linux_arm64.tar.gz"
      sha256 "84820a707d4f653cf97f779d953a9cc8d12c296fee5b0e681cbd7398ad9f2d38"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.27/aviary_v0.7.27_linux_amd64.tar.gz"
      sha256 "4cbf7c55f5e9f9b24773957fda0f4f788cf95f5e7e9df63f4a07202d063544d4"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
