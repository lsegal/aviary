class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.43"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.43/aviary_v0.7.43_darwin_arm64.tar.gz"
      sha256 "f6fc4dbdde45e9e483a17287fb41b4cdffd6ddfdcf61a9212638ef3258d68494"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.43/aviary_v0.7.43_darwin_amd64.tar.gz"
      sha256 "00507247d35533237db75900475607dd5828c88abe17906ec58d85d819a70043"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.43/aviary_v0.7.43_linux_arm64.tar.gz"
      sha256 "524c69cabdc417032fdd3bdb63bae254353a2cc3c209913aee73060bb77c02aa"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.43/aviary_v0.7.43_linux_amd64.tar.gz"
      sha256 "f048f57ed88ee268543fe287e1b24eda900bf67e6fbab099fe5a79488b79748a"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
