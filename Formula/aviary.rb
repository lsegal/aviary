# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.15'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.15/aviary_v1.0.15_darwin_arm64.tar.gz'
      sha256 'f1479eee6c11ef855a2be42ac0c4a4befd02e3fccc4556a2cdcf110e45b875ef'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.15/aviary_v1.0.15_darwin_amd64.tar.gz'
      sha256 'a406341ac69c3752c35b58bad7057164e737f3d59f966e8c8cf41270d0017d7b'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.15/aviary_v1.0.15_linux_arm64.tar.gz'
      sha256 'ba8c2ac12b448d3fd6436b2cac516e18e04f6b0d9e89679f94c5e70cbf77ae7c'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.15/aviary_v1.0.15_linux_amd64.tar.gz'
      sha256 '8dca96c804de675bedad041d29c876eed5d804bc0af403cd59a6fdd35425ab0d'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
