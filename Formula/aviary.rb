class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.46"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.46/aviary_v0.7.46_darwin_arm64.tar.gz"
      sha256 "a9432d6ba18005dc2e5acd90c08442192b0c1681bd16d4d4cad14f9f60027760"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.46/aviary_v0.7.46_darwin_amd64.tar.gz"
      sha256 "c67cbd4d34667c227a7954e7e2123a5e258f48e08fd67372e02635663b75ecdf"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.46/aviary_v0.7.46_linux_arm64.tar.gz"
      sha256 "219f4b20eaea6e7647f062b94250485d4b4880a830edbcee03bb20a13e5f2713"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.46/aviary_v0.7.46_linux_amd64.tar.gz"
      sha256 "b9df0f017283f8774bb7da6734a02a83e7b683a9a5a60aa83ee53fdc85a8f827"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
