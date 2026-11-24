class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.15"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.15/aviary_v0.7.15_darwin_arm64.tar.gz"
      sha256 "aaa16f8be393cc81b9b29ae3c75e9a85a13baabe60b366cd1eb343b69f959e48"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.15/aviary_v0.7.15_darwin_amd64.tar.gz"
      sha256 "b4e07669757e41acdf607eccc30916c9497449431ab0cce98b845d8225728b88"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.15/aviary_v0.7.15_linux_arm64.tar.gz"
      sha256 "f53bf000c954c908012dd7747d8f3dd93d274a7d0938b144c98458630454642c"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.15/aviary_v0.7.15_linux_amd64.tar.gz"
      sha256 "8401b2780a45deedfaa2d0ce09243ba77479f40c0f54abf32a297d9cfef715a1"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
